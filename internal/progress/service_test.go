package progress

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPrepareTaskEvent(t *testing.T) {
	t.Parallel()
	from := "pending"
	event, err := prepareTaskEvent(TaskEventInput{
		TaskID: 4, TaskVersion: 2, EventType: " EXECUTION_STARTED ",
		FromStatus: &from, ToStatus: "EXECUTING", ActorType: "M5",
		Detail: map[string]any{"source": "button"}, OccurredAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("prepareTaskEvent() error = %v", err)
	}
	if event.EventType != "execution_started" || event.ActorType != "m5" || event.ToStatus != "executing" {
		t.Fatalf("event = %#v", event)
	}
	if got := string(event.Detail); got != `{"source":"button"}` {
		t.Fatalf("detail = %s", got)
	}
}

func TestPrepareTaskEventRejectsUnknownType(t *testing.T) {
	t.Parallel()
	_, err := prepareTaskEvent(TaskEventInput{
		TaskID: 1, TaskVersion: 0, EventType: "guessed",
		ToStatus: "pending", ActorType: "system", OccurredAt: time.Now(),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestPrepareProjectEventRequiresSourcePair(t *testing.T) {
	t.Parallel()
	now := time.Now()
	sourceType := "feishu_message"
	_, err := prepareProjectEvent(ProjectEventInput{
		ProjectID: 1, EventType: "progress_reported", Title: "完成接口",
		ActorType: "user", SourceType: &sourceType, OccurredAt: &now,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestPrepareProjectEventCanonicalizesDetailAndKey(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	input := ProjectEventInput{
		ProjectID: 2, EventType: "milestone_reached", Title: "MVP 跑通",
		ActorType: "user", Detail: json.RawMessage(`{ "percent": 100 }`), OccurredAt: &now,
	}
	first, err := prepareProjectEvent(input)
	if err != nil {
		t.Fatalf("prepareProjectEvent() error = %v", err)
	}
	second, err := prepareProjectEvent(input)
	if err != nil {
		t.Fatalf("prepareProjectEvent() second error = %v", err)
	}
	if first.EventKey == "" || first.EventKey != second.EventKey {
		t.Fatalf("event keys = %q, %q", first.EventKey, second.EventKey)
	}
	if got := string(first.Detail); got != `{"percent":100}` {
		t.Fatalf("detail = %s", got)
	}
}
