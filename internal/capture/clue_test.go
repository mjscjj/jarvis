package capture

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Validation runs before any storage access, so a bare Service is enough to pin
// the fail-fast contract: a malformed delivery must never reach the database.
func TestAppendClueRejectsMalformedInput(t *testing.T) {
	valid := ClueInput{
		Source: "feishu_meeting", ExternalID: "7667030332496007223",
		Title: "会议结束：公会基建Agent 日会", OccurredAt: time.Now(),
	}
	cases := []struct {
		name    string
		mutate  func(*ClueInput)
		wantErr string
	}{
		{"blank source", func(in *ClueInput) { in.Source = "" }, "source"},
		{"uppercase source", func(in *ClueInput) { in.Source = "FeishuMeeting" }, "source"},
		{"source with colon", func(in *ClueInput) { in.Source = "feishu:meeting" }, "source"},
		{"source too long", func(in *ClueInput) { in.Source = strings.Repeat("a", 33) }, "source"},
		{"blank external id", func(in *ClueInput) { in.ExternalID = "  " }, "external_id"},
		{"blank title", func(in *ClueInput) { in.Title = "  " }, "title"},
		{"zero occurred at", func(in *ClueInput) { in.OccurredAt = time.Time{} }, "occurred_at"},
		{"message id too long", func(in *ClueInput) { in.ExternalID = strings.Repeat("9", 60) }, "exceeds 64 chars"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := valid
			testCase.mutate(&input)
			_, err := (&Service{}).AppendClue(context.Background(), input)
			if !errors.Is(err, ErrInvalidClue) {
				t.Fatalf("AppendClue() error = %v, want ErrInvalidClue", err)
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("AppendClue() error = %v, want it to mention %q", err, testCase.wantErr)
			}
		})
	}
}
