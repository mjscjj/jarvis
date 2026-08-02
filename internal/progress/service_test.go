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

func TestPrepareTaskEventAcceptsHumanPauseLifecycleTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		eventType string
		from      string
		to        string
		actor     string
	}{
		{eventType: "human_input_requested", from: "executing", to: "needs_human", actor: "m5"},
		{eventType: "human_response_received", from: "needs_human", to: "executing", actor: "user"},
	}
	for _, item := range cases {
		item := item
		t.Run(item.eventType, func(t *testing.T) {
			_, err := prepareTaskEvent(TaskEventInput{
				TaskID: 1, TaskVersion: 2, EventType: item.eventType,
				FromStatus: &item.from, ToStatus: item.to, ActorType: item.actor, OccurredAt: time.Now(),
			})
			if err != nil {
				t.Fatalf("prepareTaskEvent(%s) error = %v", item.eventType, err)
			}
		})
	}
}

func TestPrepareTaskEventAcceptsExecutionInterrupted(t *testing.T) {
	t.Parallel()
	from := "executing"
	_, err := prepareTaskEvent(TaskEventInput{
		TaskID: 1, TaskVersion: 3, EventType: "execution_interrupted",
		FromStatus: &from, ToStatus: "failed", ActorType: "m5", OccurredAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("prepareTaskEvent(execution_interrupted) error = %v", err)
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

func TestPrepareTaskEventRejectsUnknownActor(t *testing.T) {
	t.Parallel()
	_, err := prepareTaskEvent(TaskEventInput{
		TaskID: 1, TaskVersion: 0, EventType: "created",
		ToStatus: "pending", ActorType: "unknown_stage", OccurredAt: time.Now(),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestPrepareFactUsesNaturalLanguage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	fact, err := prepareFact(FactInput{
		SubjectType: " Project ", SubjectID: 2,
		Description: "  MVP 已跑通，下一步部署测试环境。  ", OccurredAt: &now,
	})
	if err != nil {
		t.Fatalf("prepareFact() error = %v", err)
	}
	if fact.Description != "MVP 已跑通，下一步部署测试环境。" || !fact.OccurredAt.Equal(now.UTC()) {
		t.Fatalf("fact = %#v", fact)
	}
	if fact.SubjectType != "project" {
		t.Fatalf("SubjectType = %q, want normalized to project", fact.SubjectType)
	}
}

func TestPrepareFactRequiresDescriptionAndSubject(t *testing.T) {
	t.Parallel()
	now := time.Now()
	if _, err := prepareFact(FactInput{SubjectType: "project", SubjectID: 1, OccurredAt: &now}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing description error = %v, want ErrInvalidInput", err)
	}
	if _, err := prepareFact(FactInput{SubjectID: 1, Description: "x", OccurredAt: &now}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing subject_type error = %v, want ErrInvalidInput", err)
	}
}

// TestPrepareFactKeepsUnknownSubjectType pins the decision that SubjectType is
// not an enum: a type the system has no table for is still stored.
func TestPrepareFactKeepsUnknownSubjectType(t *testing.T) {
	t.Parallel()
	now := time.Now()
	fact, err := prepareFact(FactInput{
		SubjectType: "meeting", SubjectID: 9, Description: "评审会决定砍掉旁路", OccurredAt: &now,
	})
	if err != nil {
		t.Fatalf("prepareFact() with unknown subject type error = %v, want stored", err)
	}
	if fact.SubjectType != "meeting" {
		t.Fatalf("SubjectType = %q, want meeting", fact.SubjectType)
	}
	if _, ok := factSubjectModel("meeting"); ok {
		t.Fatal("factSubjectModel(meeting) = ok, want no parent table")
	}
}
