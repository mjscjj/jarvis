package insight

import (
	"context"
	"errors"
	"testing"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMeetingReviewProjectsExplicitMeetingSummary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:meeting-review?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.Group{}, &domain.Message{}, &domain.Todo{}, &domain.Task{}, &domain.ExecutionRun{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	location := time.FixedZone("UTC+8", 8*60*60)
	service, err := NewMeetingReviewService(db, location)
	if err != nil {
		t.Fatalf("NewMeetingReviewService() error = %v", err)
	}

	groupName := "线索：feishu_meeting"
	group := domain.Group{ChatID: meetingClueChatID, ChatMode: "clue", Name: &groupName, RelatedGroup: true, Tier: "hot"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create clue group: %v", err)
	}
	createMeetingClue := func(messageID, title, startAt, endAt string) domain.Message {
		message := domain.Message{
			MessageID: messageID, ChatID: meetingClueChatID, GroupID: &group.ID, ChatMode: "clue",
			SenderOpenID: "__clue__", SenderName: "feishu_meeting", SenderType: "system",
			MessageType: "clue", Source: "clue", RenderOK: true, CreateTime: time.Now().UnixMilli(),
			Content: "会议结束：" + title + "\n" +
				"线索发生时间：" + endAt + "\n" +
				"会议主题：" + title + "\n" +
				"会议 ID：" + messageID[len(meetingClueChatID)+1:] + "\n" +
				"开始时间：" + startAt + "\n" +
				"结束时间：" + endAt + "\n" +
				"主持人：张三（ou_zhangsan）\n" +
				"参会人：张三、李四\n" +
				"会议链接：https://example.com/meeting",
		}
		if err := db.Create(&message).Error; err != nil {
			t.Fatalf("create meeting clue: %v", err)
		}
		return message
	}
	first := createMeetingClue(
		"clue:feishu_meeting:meeting-1", "架构评审",
		"2026-08-07T10:00:00+08:00", "2026-08-07T11:00:00+08:00",
	)
	createMeetingClue(
		"clue:feishu_meeting:meeting-2", "午后同步",
		"2026-08-07T14:00:00+08:00", "2026-08-07T14:30:00+08:00",
	)
	createMeetingClue(
		"clue:feishu_meeting:meeting-old", "昨天的会",
		"2026-08-06T14:00:00+08:00", "2026-08-06T14:30:00+08:00",
	)

	now := time.Now()
	todo := domain.Todo{
		Title: "整理架构评审", Description: "整理会议结果", ActionType: "summary_post",
		Target: "架构评审（meeting_id=meeting-1）", Context: "", OpenQuestions: datatypes.JSON(`[]`),
		CommitmentStrength: "explicit", SourceMessageIDs: datatypes.JSON(`["` + first.MessageID + `"]`),
		SourceQuote: "会议结束", GroupID: &group.ID, Status: "materialized", DedupFingerprint: "meeting-review-1",
		FirstSeenAt: now, LastEvidenceAt: now,
	}
	if err := db.Create(&todo).Error; err != nil {
		t.Fatalf("create Todo: %v", err)
	}
	task := domain.Task{
		TodoID: &todo.ID, Title: todo.Title, ActionType: todo.ActionType, Target: todo.Target,
		Background: datatypes.JSON(`{}`), SourcePayload: datatypes.JSON(`{}`), SourceType: "todo",
		Status: "done",
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create Task: %v", err)
	}
	finishedAt := time.Date(2026, 8, 7, 11, 5, 0, 0, location)
	summaryRun := domain.ExecutionRun{
		TaskID: task.ID, ActionType: "summary_post", Stage: "execute", Sandbox: "read-only", Status: "succeeded",
		Prompt: "summarize", StartedAt: finishedAt.Add(-time.Minute), FinishedAt: &finishedAt,
		Output:  datatypes.JSON(`{"enrichments":[{"kind":"meeting_summary","label":"会议总结","content":"决定采用方案 B。"}]}`),
		Effects: datatypes.JSON(`[{"kind":"doc","title":"会议纪要","url":"https://example.com/doc"}]`),
	}
	if err := db.Create(&summaryRun).Error; err != nil {
		t.Fatalf("create summary run: %v", err)
	}
	newerRun := domain.ExecutionRun{
		TaskID: task.ID, ActionType: "summary_post", Stage: "execute", Sandbox: "read-only", Status: "succeeded",
		Prompt: "follow up", StartedAt: finishedAt.Add(time.Hour), Output: datatypes.JSON(`{"enrichments":[]}`),
	}
	if err := db.Create(&newerRun).Error; err != nil {
		t.Fatalf("create newer run: %v", err)
	}

	result, err := service.Load(context.Background(), "2026-08-07")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("Load() items = %d, want 2", len(result.Items))
	}
	got := result.Items[0]
	if got.MeetingID != "meeting-1" || got.Title != "架构评审" || got.TaskID == nil || *got.TaskID != task.ID || got.TaskStatus != "done" {
		t.Fatalf("first meeting = %+v", got)
	}
	if got.Summary != "决定采用方案 B。" || got.SummaryGeneratedAt == nil || !got.SummaryGeneratedAt.Equal(finishedAt) {
		t.Fatalf("summary = %q generated_at=%v", got.Summary, got.SummaryGeneratedAt)
	}
	if len(got.Effects) != 1 || got.Effects[0]["url"] != "https://example.com/doc" {
		t.Fatalf("effects = %#v", got.Effects)
	}
	if result.Items[1].MeetingID != "meeting-2" || result.Items[1].Summary != "" || result.Items[1].TaskID != nil {
		t.Fatalf("second meeting = %+v", result.Items[1])
	}
}

func TestMeetingReviewRejectsInvalidDate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:meeting-review-invalid?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	service, err := NewMeetingReviewService(db, time.UTC)
	if err != nil {
		t.Fatalf("NewMeetingReviewService() error = %v", err)
	}
	if _, err := service.Load(context.Background(), "2026-8-7"); !errors.Is(err, ErrInvalidReviewDate) {
		t.Fatalf("Load() error = %v, want ErrInvalidReviewDate", err)
	}
}
