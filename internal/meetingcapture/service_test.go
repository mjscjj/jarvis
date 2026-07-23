package meetingcapture

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuildEvidenceIncludesArtifactsAndCapsContent(t *testing.T) {
	detail := meetingDetail{
		MeetingID: "7665545620547210872", Topic: "Bax工具方案review",
		StartTime: "2026-07-23 11:00", EndTime: "2026-07-23 12:27",
		MinuteToken: "obsgy64ntofeq3l77291lvjo",
	}
	minute := minuteDetail{}
	minute.Artifacts.Todos = json.RawMessage(`[{"content":"储节节补充工具方案","owner":"储节节"}]`)
	transcript := strings.Repeat("讨论过程。", 200)

	got, err := buildEvidence(detail, "https://example.test/meeting", minute, transcript, 500)
	if err != nil {
		t.Fatalf("buildEvidence() error = %v", err)
	}
	if !strings.Contains(got, "Bax工具方案review") || !strings.Contains(got, "储节节补充工具方案") {
		t.Fatalf("buildEvidence() missing meeting or Todo evidence: %s", got)
	}
	if count := len([]rune(got)); count > 500 {
		t.Fatalf("buildEvidence() runes = %d, want <= 500", count)
	}
	if !strings.Contains(got, "上下文上限截断") {
		t.Fatalf("buildEvidence() did not mark truncation: %s", got)
	}
}

func TestBuildPermissionCaptureResultContainsFactsWithoutDecision(t *testing.T) {
	detail := meetingDetail{
		MeetingID: "7665545620547210872", Topic: "Bax工具方案review",
		StartTime: "2026-07-23 11:00", EndTime: "2026-07-23 12:27",
		MinuteToken: "obsgy64ntofeq3l77291lvjo",
	}
	got, err := buildPermissionCaptureResult(
		detail,
		"https://example.test/meeting",
		errors.New("No read permission for minute obsgy64ntofeq3l77291lvjo"),
	)
	if err != nil {
		t.Fatalf("buildPermissionCaptureResult() error = %v", err)
	}
	for _, want := range []string{
		"[会议妙记采集结果]",
		"采集结果：permission_denied",
		"错误原文：No read permission for minute obsgy64ntofeq3l77291lvjo",
		"采集模块未申请权限，也未决定后续处理方式",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("buildPermissionCaptureResult() missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"需要 principal 处理", "请决定是否申请", "批准后可执行"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("buildPermissionCaptureResult() contains decision %q: %s", forbidden, got)
		}
	}
	if got := meetingCaptureResultMessageID(detail.MeetingID); got != "vc_meeting_capture_7665545620547210872" {
		t.Fatalf("meetingCaptureResultMessageID() = %q", got)
	}
}

func TestSearchMeetingsPaginatesAndDeduplicates(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	fake := &searchFixture{}
	service := &Service{
		lark: fake,
		opts: Options{
			PrincipalOpenID: "ou_owner", LookbackDays: 2, Location: location,
		},
	}
	items, err := service.searchMeetings(context.Background(), time.Date(2026, 7, 23, 13, 0, 0, 0, location))
	if err != nil {
		t.Fatalf("searchMeetings() error = %v", err)
	}
	if len(items) != 2 || items[0].ID != "meeting-1" || items[1].ID != "meeting-2" {
		t.Fatalf("searchMeetings() items = %#v", items)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("searchMeetings() calls = %d, want 2", len(fake.calls))
	}
	first := strings.Join(fake.calls[0], " ")
	if !strings.Contains(first, "--start 2026-07-22") || !strings.Contains(first, "--end 2026-07-23") ||
		!strings.Contains(first, "--participant-ids ou_owner") {
		t.Fatalf("first search args = %q", first)
	}
	if second := strings.Join(fake.calls[1], " "); !strings.Contains(second, "--page-token next-page") {
		t.Fatalf("second search args = %q", second)
	}
}

func TestMeetingHelpersFailFast(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	got, err := parseMeetingTime("2026-07-23 12:27", location)
	if err != nil {
		t.Fatalf("parseMeetingTime() error = %v", err)
	}
	if got.Hour() != 12 || got.Minute() != 27 {
		t.Fatalf("parseMeetingTime() = %s", got)
	}
	if _, err := parseMeetingTime("2026/07/23 12:27", location); err == nil {
		t.Fatal("parseMeetingTime() accepted unsupported layout")
	}
	if !isPermissionError(errors.New("No read permission for minute")) {
		t.Fatal("isPermissionError() missed permission error")
	}
	if isPermissionError(errors.New("rate limited")) {
		t.Fatal("isPermissionError() misclassified rate limit")
	}
}

func TestPermissionDeniedRetriesWithinMeetingScanWindow(t *testing.T) {
	if permissionRetryDelay != 10*time.Minute {
		t.Fatalf("permissionRetryDelay = %s, want 10m", permissionRetryDelay)
	}
}

type searchFixture struct {
	calls [][]string
}

func (f *searchFixture) Run(_ context.Context, out any, args ...string) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	response := out.(*searchResponse)
	if len(f.calls) == 1 {
		response.Data.Items = []searchItem{{ID: "meeting-1"}}
		response.Data.HasMore = true
		response.Data.PageToken = "next-page"
		return nil
	}
	response.Data.Items = []searchItem{{ID: "meeting-1"}, {ID: "meeting-2"}}
	return nil
}
