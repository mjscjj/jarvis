package proactive

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	prompt, sandbox, root, stage string
	result                       string
	err                          error
}

func (f *fakeRunner) RunTextSandboxAtStage(_ context.Context, prompt, sandbox, root, stage string) (string, error) {
	f.prompt, f.sandbox, f.root, f.stage = prompt, sandbox, root, stage
	return f.result, f.err
}

type fakePromptReader struct {
	text string
	err  error
}

func (f fakePromptReader) Content(context.Context, string) (string, error) { return f.text, f.err }

type fakeMemoryReader struct {
	text string
	err  error
}

func (f fakeMemoryReader) Text(context.Context) (string, error) { return f.text, f.err }

type fakeRuleReader struct {
	text string
	err  error
}

func (f fakeRuleReader) Block(context.Context, string) (string, error) { return f.text, f.err }

func TestWorkerBuildsHeartbeatPromptAndUsesProactiveStage(t *testing.T) {
	runner := &fakeRunner{result: "NOTHING：本轮没有值得推进的事项"}
	worker, err := NewWorker(Options{
		Runner: runner, Prompts: fakePromptReader{text: "system mission"},
		SharedMemory: fakeMemoryReader{text: "trusted memory"},
		WorkRules:    fakeRuleReader{text: "global rules"}, Sandbox: "danger-full-access",
		WorkspaceRoot: "/tmp/jarvis", Location: time.FixedZone("CST", 8*60*60),
	})
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return time.Date(2026, 8, 2, 15, 4, 5, 0, time.UTC) }
	result, err := worker.RunOnce(t.Context())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result != runner.result {
		t.Fatalf("result = %q", result)
	}
	for _, want := range []string{"system mission", "global rules", "trusted memory", "BEGIN_AVAILABLE_TOOLS", "BEGIN_HEARTBEAT", "2026-08-02T23:04:05+08:00"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
	if runner.stage != AgentStage || runner.sandbox != "danger-full-access" || runner.root != "/tmp/jarvis" {
		t.Fatalf("runner args stage=%q sandbox=%q root=%q", runner.stage, runner.sandbox, runner.root)
	}
}

func TestWorkerFailsOnDependencyOrEmptyResult(t *testing.T) {
	base := Options{
		Runner: &fakeRunner{result: "ok"}, Prompts: fakePromptReader{text: "system"},
		SharedMemory: fakeMemoryReader{}, WorkRules: fakeRuleReader{},
		Sandbox: "danger-full-access", WorkspaceRoot: "/tmp/jarvis", Location: time.UTC,
	}
	worker, err := NewWorker(base)
	if err != nil {
		t.Fatal(err)
	}
	worker.prompts = fakePromptReader{err: errors.New("missing")}
	if _, err := worker.RunOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "read proactive system prompt") {
		t.Fatalf("prompt error = %v", err)
	}
	worker.prompts = fakePromptReader{text: "system"}
	worker.runner = &fakeRunner{result: "  "}
	if _, err := worker.RunOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "empty final message") {
		t.Fatalf("empty result error = %v", err)
	}
}
