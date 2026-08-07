package capture

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"
)

func TestTopicScanCapturesNewRepliesUnderOldRoot(t *testing.T) {
	location := mustShanghai(t)
	windowStart := time.Date(2026, 8, 7, 12, 25, 0, 0, location)
	now := time.Date(2026, 8, 7, 12, 45, 0, 0, location)
	db := newCaptureTestDB(t)
	createDiscoveredGroup(t, db, "oc_topic", "topic", true, windowStart)

	runner := &topicSearchFixture{pages: map[string]topicSearchPage{
		"": {
			messages: []CLIMessage{
				topicMessage("om_reply_40", "omt_old", "jarvis，你来分析下", "2026-08-07 12:40"),
				topicMessage("om_reply_30", "omt_old", "补充截图", "2026-08-07 12:30"),
			},
			hasMore:   true,
			pageToken: "page-2",
		},
		"page-2": {
			messages: []CLIMessage{
				topicMessage("om_reply_31", "omt_old", "没权限？", "2026-08-07 12:31"),
			},
		},
	}}
	service := newPrincipalActivityService(t, db, runner, location)
	service.now = func() time.Time { return now }
	observer := &activityRecordingObserver{}
	if err := service.SetScanObserver(observer); err != nil {
		t.Fatalf("SetScanObserver() error = %v", err)
	}

	if err := service.ScanChat(context.Background(), "oc_topic"); err != nil {
		t.Fatalf("ScanChat() error = %v", err)
	}

	var messages []domain.Message
	if err := db.Where("chat_id = ?", "oc_topic").Order("create_time ASC, message_id ASC").Find(&messages).Error; err != nil {
		t.Fatalf("list stored topic messages: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("stored message count = %d, want 3", len(messages))
	}
	if messages[0].MessageID != "om_reply_30" || messages[2].MessageID != "om_reply_40" {
		t.Fatalf("stored message order = %#v", []string{messages[0].MessageID, messages[1].MessageID, messages[2].MessageID})
	}
	for _, message := range messages {
		if message.ThreadID == nil || *message.ThreadID != "omt_old" {
			t.Fatalf("message %s thread_id = %v, want omt_old", message.MessageID, message.ThreadID)
		}
	}
	var checkpoint domain.Checkpoint
	if err := db.First(&checkpoint, "chat_id = ?", "oc_topic").Error; err != nil {
		t.Fatalf("load topic checkpoint: %v", err)
	}
	if checkpoint.HighWaterCreateTime != time.Date(2026, 8, 7, 12, 40, 0, 0, location).UnixMilli() {
		t.Fatalf("topic high water = %d, want 12:40", checkpoint.HighWaterCreateTime)
	}
	var scan domain.ScanRecord
	if err := db.Where("chat_id = ?", "oc_topic").Order("id DESC").First(&scan).Error; err != nil {
		t.Fatalf("load topic scan record: %v", err)
	}
	wantSearchStart := windowStart.Add(-10 * time.Minute).UnixMilli()
	if scan.WindowStart == nil || *scan.WindowStart != wantSearchStart {
		t.Fatalf("topic scan window_start = %v, want %d", scan.WindowStart, wantSearchStart)
	}
	if scan.HighWaterBefore == nil || *scan.HighWaterBefore != windowStart.UnixMilli() {
		t.Fatalf("topic scan high_water_before = %v, want %d", scan.HighWaterBefore, windowStart.UnixMilli())
	}
	if len(observer.results) != 1 || observer.results[0].InsertedCount != 3 {
		t.Fatalf("scan observer results = %#v", observer.results)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("topic search calls = %d, want 2", len(runner.calls))
	}
	firstArgs := strings.Join(runner.calls[0], " ")
	for _, required := range []string{
		"+messages-search",
		"--query  --chat-id oc_topic",
		"--start 2026-08-07T12:15:00+08:00",
		"--end 2026-08-07T12:45:00+08:00",
		"--no-reactions",
		"--as user",
	} {
		if !strings.Contains(firstArgs, required) {
			t.Errorf("first search args %q do not contain %q", firstArgs, required)
		}
	}
	if strings.Contains(firstArgs, "--page-all") {
		t.Fatalf("topic search unexpectedly used capped --page-all: %q", firstArgs)
	}
	if got := argValue(runner.calls[1], "--page-token"); got != "page-2" {
		t.Fatalf("second search page token = %q, want page-2", got)
	}

	// The overlap intentionally returns the same messages again. message_id
	// idempotency must prevent duplicate rows and duplicate M3 wakeups.
	if err := service.ScanChat(context.Background(), "oc_topic"); err != nil {
		t.Fatalf("second ScanChat() error = %v", err)
	}
	var count int64
	if err := db.Model(&domain.Message{}).Where("chat_id = ?", "oc_topic").Count(&count).Error; err != nil {
		t.Fatalf("count stored topic messages: %v", err)
	}
	if count != 3 {
		t.Fatalf("stored message count after overlap = %d, want 3", count)
	}
	if len(observer.results) != 1 {
		t.Fatalf("overlap emitted duplicate observer result: %#v", observer.results)
	}
}

func TestTopicScanPaginationFailureDoesNotAdvanceCheckpoint(t *testing.T) {
	location := mustShanghai(t)
	windowStart := time.Date(2026, 8, 7, 12, 25, 0, 0, location)
	now := time.Date(2026, 8, 7, 12, 45, 0, 0, location)
	db := newCaptureTestDB(t)
	createDiscoveredGroup(t, db, "oc_topic_failure", "topic", true, windowStart)

	runner := &topicSearchFixture{
		pages: map[string]topicSearchPage{
			"": {
				messages:  []CLIMessage{topicMessage("om_uncommitted", "omt_old", "later reply", "2026-08-07 12:40")},
				hasMore:   true,
				pageToken: "page-2",
			},
		},
		failPageToken: "page-2",
		failErr:       errors.New("search page unavailable"),
	}
	service := newPrincipalActivityService(t, db, runner, location)
	service.now = func() time.Time { return now }

	err := service.ScanChat(context.Background(), "oc_topic_failure")
	if err == nil || !strings.Contains(err.Error(), "page=2") || !strings.Contains(err.Error(), "search page unavailable") {
		t.Fatalf("ScanChat() error = %v, want second-page failure", err)
	}
	var count int64
	if err := db.Model(&domain.Message{}).Where("chat_id = ?", "oc_topic_failure").Count(&count).Error; err != nil {
		t.Fatalf("count stored topic messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("stored message count after partial search = %d, want 0", count)
	}
	var checkpoint domain.Checkpoint
	if err := db.First(&checkpoint, "chat_id = ?", "oc_topic_failure").Error; err != nil {
		t.Fatalf("load topic checkpoint: %v", err)
	}
	if checkpoint.HighWaterCreateTime != windowStart.UnixMilli() {
		t.Fatalf("topic high water after failure = %d, want %d", checkpoint.HighWaterCreateTime, windowStart.UnixMilli())
	}
	if checkpoint.LastScanStatus == nil || *checkpoint.LastScanStatus != "error" {
		t.Fatalf("topic last scan status = %v, want error", checkpoint.LastScanStatus)
	}
	var scan domain.ScanRecord
	if err := db.Where("chat_id = ?", "oc_topic_failure").Order("id DESC").First(&scan).Error; err != nil {
		t.Fatalf("load failed topic scan record: %v", err)
	}
	if scan.FetchedCount != 1 || scan.PageCount != 1 || scan.Status != "error" {
		t.Fatalf("failed topic scan record = %#v, want fetched=1 pages=1 status=error", scan)
	}
}

func TestTopicScanRejectsRepeatedPageToken(t *testing.T) {
	location := mustShanghai(t)
	windowStart := time.Date(2026, 8, 7, 12, 25, 0, 0, location)
	db := newCaptureTestDB(t)
	createDiscoveredGroup(t, db, "oc_topic_repeated_token", "topic", true, windowStart)

	runner := &topicSearchFixture{pages: map[string]topicSearchPage{
		"":       {hasMore: true, pageToken: "repeat"},
		"repeat": {hasMore: true, pageToken: "repeat"},
	}}
	service := newPrincipalActivityService(t, db, runner, location)
	service.now = func() time.Time { return windowStart.Add(20 * time.Minute) }

	err := service.ScanChat(context.Background(), "oc_topic_repeated_token")
	if err == nil || !strings.Contains(err.Error(), `repeated page_token="repeat"`) {
		t.Fatalf("ScanChat() error = %v, want repeated page token failure", err)
	}
	var checkpoint domain.Checkpoint
	if err := db.First(&checkpoint, "chat_id = ?", "oc_topic_repeated_token").Error; err != nil {
		t.Fatalf("load topic checkpoint: %v", err)
	}
	if checkpoint.HighWaterCreateTime != windowStart.UnixMilli() {
		t.Fatalf("topic high water after repeated token = %d, want %d", checkpoint.HighWaterCreateTime, windowStart.UnixMilli())
	}
}

type topicSearchPage struct {
	messages  []CLIMessage
	hasMore   bool
	pageToken string
}

type topicSearchFixture struct {
	pages         map[string]topicSearchPage
	failPageToken string
	failErr       error
	calls         [][]string
}

func (f *topicSearchFixture) Run(_ context.Context, out any, args ...string) error {
	if !strings.Contains(strings.Join(args, " "), "+messages-search") {
		return fmt.Errorf("unexpected lark-cli args: %s", strings.Join(args, " "))
	}
	f.calls = append(f.calls, append([]string(nil), args...))
	pageToken := argValue(args, "--page-token")
	if pageToken == f.failPageToken && f.failErr != nil {
		return f.failErr
	}
	page, ok := f.pages[pageToken]
	if !ok {
		return fmt.Errorf("unexpected page token %q", pageToken)
	}
	response, ok := out.(*MessageSearchListResponse)
	if !ok {
		return fmt.Errorf("unexpected response type %T", out)
	}
	response.OK = true
	response.Data.Messages = append([]CLIMessage(nil), page.messages...)
	response.Data.HasMore = page.hasMore
	response.Data.PageToken = page.pageToken
	response.Data.Total = len(page.messages)
	return nil
}

func topicMessage(messageID, threadID, content, createTime string) CLIMessage {
	return CLIMessage{
		ChatID:      "oc_topic",
		Content:     content,
		CreateTime:  createTime,
		MessageID:   messageID,
		MessageType: "text",
		ThreadID:    threadID,
		Sender:      CLISender{ID: "ou_principal", Name: "principal", SenderType: "user"},
	}
}
