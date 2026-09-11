package chat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestSessionRunningAndRejectedInputRemainIntact(t *testing.T) {
	svc := newPersistentTestService(t)
	session, err := svc.CreateSession(t.Context(), CreateSessionInput{Agent: "codex", Model: "gpt-5.5", ReasoningEffort: "high", Draft: json.RawMessage(`{"text":"keep draft"}`)})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	svc.active[session.ID] = cancel
	view, err := svc.GetSession(t.Context(), session.ID)
	if err != nil || !view.Running {
		t.Fatalf("running view: %+v, %v", view, err)
	}
	list, err := svc.ListSessions(t.Context(), "", false)
	if err != nil || len(list) != 1 || !list[0].Running {
		t.Fatalf("running list: %+v, %v", list, err)
	}
	err = svc.StreamSession(t.Context(), session.ID, SendInput{Message: "new input"}, func(Event) error {
		t.Fatal("rejected input must not be acknowledged")
		return nil
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	view, err = svc.GetSession(t.Context(), session.ID)
	if err != nil || len(view.Messages) != 0 || string(view.Draft) != `{"text":"keep draft"}` {
		t.Fatalf("rejected input changed session: %+v, %v", view, err)
	}
	if !svc.CancelSession(session.ID) || runCtx.Err() != context.Canceled {
		t.Fatal("cancel did not reach the active reply")
	}
	delete(svc.active, session.ID) // Runner cleanup removes the in-memory state.
	view, err = svc.GetSession(t.Context(), session.ID)
	if err != nil || view.Running {
		t.Fatalf("completed view: %+v, %v", view, err)
	}
}

func TestAcceptedEventFollowsMessageAndAttachmentPersistence(t *testing.T) {
	svc := newPersistentTestService(t)
	session, err := svc.CreateSession(t.Context(), CreateSessionInput{Agent: "codex", Model: "gpt-5.5", ReasoningEffort: "high", Draft: json.RawMessage(`{"text":"hello"}`)})
	if err != nil {
		t.Fatal(err)
	}
	file, err := svc.SaveUpload(t.Context(), session.ID, "note.txt", "text/plain", 5, func(path string) error { return os.WriteFile(path, []byte("hello"), 0o600) })
	if err != nil {
		t.Fatal(err)
	}
	stopBeforeCLI := errors.New("test stops after acceptance")
	accepted := false
	err = svc.StreamSession(t.Context(), session.ID, SendInput{Message: "hello", AttachmentIDs: []string{file.ID}}, func(event Event) error {
		if event.Kind != EventAccepted {
			t.Fatalf("first event = %s", event.Kind)
		}
		accepted = true
		view, err := svc.GetSession(t.Context(), session.ID)
		if err != nil || !view.Running || len(view.Messages) != 1 || view.Messages[0].Text != "hello" || len(view.Messages[0].Attachments) != 1 || len(view.PendingAttachments) != 0 || string(view.Draft) != "{}" {
			t.Fatalf("acceptance before persistence: %+v, %v", view, err)
		}
		return stopBeforeCLI
	})
	if !accepted || !errors.Is(err, stopBeforeCLI) || svc.sessionRunning(session.ID) {
		t.Fatalf("acceptance/cleanup: %v, %v", accepted, err)
	}
}
