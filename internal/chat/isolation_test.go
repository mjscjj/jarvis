package chat

import (
	"context"
	"strings"
	"testing"
)

type isolatedTestRuntime struct {
	called bool
	prompt string
}

func (r *isolatedTestRuntime) DeleteSession(string) error { return nil }

func (r *isolatedTestRuntime) Prepare(req Request) (Request, error) { return req, nil }
func (r *isolatedTestRuntime) Stream(_ context.Context, _ Request, prompt string, _ func(Event) error) error {
	r.called = true
	r.prompt = prompt
	return nil
}

func TestIsolatedRuntimeCannotSwitchBackToHost(t *testing.T) {
	s := newPersistentTestService(t)
	r := &isolatedTestRuntime{}
	s.runtime = r
	s.toolBlock = "ONLY_OKR_TOOLS"
	err := s.Stream(t.Context(), Request{Message: "read OKR", Agent: "codex", Model: "gpt-5.5"}, func(Event) error { return nil })
	if err != nil || !r.called {
		t.Fatalf("external runtime not used: %v", err)
	}
	if strings.Contains(r.prompt, "BEGIN_AVAILABLE_TOOLS") || !strings.Contains(r.prompt, "ONLY_OKR_TOOLS") {
		t.Fatalf("host catalog leaked: %s", r.prompt)
	}
	r.called = false
	if err := s.Stream(t.Context(), Request{Message: "read OKR", Agent: "trae"}, func(Event) error { return nil }); err == nil || r.called {
		t.Fatal("agent override escaped isolated runtime")
	}
	if _, err := s.CreateSession(t.Context(), CreateSessionInput{Agent: "trae", Model: "gpt-5.5", ReasoningEffort: "medium"}); err == nil {
		t.Fatal("created unsupported engine")
	}
}

func TestIndependentDatabaseRejectsOrdinarySessionAndFork(t *testing.T) {
	ordinary, isolated := newPersistentTestService(t), newPersistentTestService(t)
	input := CreateSessionInput{Agent: "codex", Model: "gpt-5.5", ReasoningEffort: "medium"}
	session, err := ordinary.CreateSession(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := isolated.GetSession(t.Context(), session.ID); err == nil {
		t.Fatal("ordinary session visible")
	}
	input.FromSessionID = session.ID
	if _, err := isolated.CreateSession(t.Context(), input); err == nil {
		t.Fatal("ordinary history could be forked into isolated chat")
	}
}
