package meetingsweep

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

func TestWorkerBuildsHeartbeatPromptAndUsesMeetingSweepStage(t *testing.T) {
	runner := &fakeRunner{result: "本轮没有会后或会前会议"}
	worker, err := NewWorker(Options{
		Runner: runner, Prompts: fakePromptReader{text: "collector mission"},
		Sandbox: "danger-full-access", WorkspaceRoot: "/tmp/jarvis",
		Location: time.FixedZone("CST", 8*60*60), Engine: "traex", Model: "DeepSeek-V4-Flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return time.Date(2026, 8, 2, 15, 4, 5, 0, time.UTC) }
	result, err := worker.Run(t.Context(), TriggerSchedule)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != runner.result {
		t.Fatalf("result = %q", result)
	}
	for _, want := range []string{"collector mission", "BEGIN_AVAILABLE_TOOLS", "BEGIN_HEARTBEAT", "2026-08-02T23:04:05+08:00", "未来 24 小时"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
	if runner.stage != AgentStage || runner.sandbox != "danger-full-access" || runner.root != "/tmp/jarvis" {
		t.Fatalf("runner args stage=%q sandbox=%q root=%q", runner.stage, runner.sandbox, runner.root)
	}
}

func TestWorkerRejectsUnknownTrigger(t *testing.T) {
	worker := mustWorker(t, &fakeRunner{result: "ok"})
	if _, err := worker.Run(t.Context(), "cron"); err == nil || !strings.Contains(err.Error(), "trigger must be") {
		t.Fatalf("trigger error = %v", err)
	}
}

func TestWorkerFailsOnDependencyOrEmptyResult(t *testing.T) {
	worker := mustWorker(t, &fakeRunner{result: "ok"})
	worker.prompts = fakePromptReader{err: errors.New("missing")}
	if _, err := worker.RunOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "read meeting sweep system prompt") {
		t.Fatalf("prompt error = %v", err)
	}
	worker.prompts = fakePromptReader{text: "system"}
	worker.runner = &fakeRunner{result: "  "}
	if _, err := worker.RunOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "empty final message") {
		t.Fatalf("empty result error = %v", err)
	}
}

func mustWorker(t *testing.T, runner Runner) *Worker {
	t.Helper()
	worker, err := NewWorker(Options{
		Runner: runner, Prompts: fakePromptReader{text: "system"},
		Sandbox: "danger-full-access", WorkspaceRoot: "/tmp/jarvis", Location: time.UTC,
		Engine: "traex", Model: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}
