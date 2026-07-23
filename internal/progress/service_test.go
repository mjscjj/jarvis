package progress

import (
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

func TestPrepareTaskEventAcceptsScheduledTaskActor(t *testing.T) {
	t.Parallel()
	event, err := prepareTaskEvent(TaskEventInput{
		TaskID: 1, TaskVersion: 0, EventType: "created",
		ToStatus: "pending", ActorType: "scheduled_task", OccurredAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("prepareTaskEvent() error = %v", err)
	}
	if event.ActorType != "scheduled_task" {
		t.Fatalf("actor_type = %q", event.ActorType)
	}
}

func TestPrepareTaskEventAcceptsWaitingLifecycleTypes(t *testing.T) {
	t.Parallel()
	for _, eventType := range []string{"waiting_scheduled", "resumed"} {
		eventType := eventType
		t.Run(eventType, func(t *testing.T) {
			_, err := prepareTaskEvent(TaskEventInput{
				TaskID: 1, TaskVersion: 2, EventType: eventType,
				ToStatus: "waiting", ActorType: "scheduled_task", OccurredAt: time.Now(),
			})
			if err != nil {
				t.Fatalf("prepareTaskEvent(%s) error = %v", eventType, err)
			}
		})
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

func TestPrepareProjectEventUsesNaturalLanguage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	event, err := prepareProjectEvent(ProjectEventInput{
		ProjectID: 2, Description: "  MVP 已跑通，下一步部署测试环境。  ", OccurredAt: &now,
	})
	if err != nil {
		t.Fatalf("prepareProjectEvent() error = %v", err)
	}
	if event.Description != "MVP 已跑通，下一步部署测试环境。" || !event.OccurredAt.Equal(now.UTC()) {
		t.Fatalf("event = %#v", event)
	}
}

func TestPrepareProjectEventRequiresDescription(t *testing.T) {
	t.Parallel()
	now := time.Now()
	_, err := prepareProjectEvent(ProjectEventInput{ProjectID: 1, OccurredAt: &now})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}
