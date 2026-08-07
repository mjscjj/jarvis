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

type fakeRecorder struct {
	id                               uint64
	trigger, engine, model, input    string
	successOutput, failureDetail     string
	startErr, successErr, failureErr error
}

func (f *fakeRecorder) Start(_ context.Context, trigger, engine, model, input string, _ time.Time) (uint64, error) {
	f.trigger, f.engine, f.model, f.input = trigger, engine, model, input
	if f.startErr != nil {
		return 0, f.startErr
	}
	if f.id == 0 {
		f.id = 42
	}
	return f.id, nil
}

func (f *fakeRecorder) Succeed(_ context.Context, _ uint64, output string, _ time.Time) error {
	f.successOutput = output
	return f.successErr
}

func (f *fakeRecorder) Fail(_ context.Context, _ uint64, detail string, _ time.Time) error {
	f.failureDetail = detail
	return f.failureErr
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

func TestWorkerBuildsHeartbeatPromptAndUsesProactiveStage(t *testing.T) {
	runner := &fakeRunner{result: "NOTHING：本轮没有值得推进的事项"}
	recorder := &fakeRecorder{}
	worker, err := NewWorker(Options{
		Runner: runner, Recorder: recorder, Prompts: fakePromptReader{text: "system mission"},
		SharedMemory:  fakeMemoryReader{text: "trusted memory"},
		Sandbox:       "danger-full-access",
		WorkspaceRoot: "/tmp/jarvis", Location: time.FixedZone("CST", 8*60*60),
		Engine: "traex", Model: "DeepSeek-V4-Pro",
	})
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return time.Date(2026, 8, 2, 15, 4, 5, 0, time.UTC) }
	result, err := worker.Run(t.Context(), TriggerSchedule)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result != runner.result {
		t.Fatalf("result = %q", result)
	}
	for _, want := range []string{"system mission", "trusted memory", "BEGIN_AVAILABLE_TOOLS", "BEGIN_HEARTBEAT", "2026-08-02T23:04:05+08:00"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
	if runner.stage != AgentStage || runner.sandbox != "danger-full-access" || runner.root != "/tmp/jarvis" {
		t.Fatalf("runner args stage=%q sandbox=%q root=%q", runner.stage, runner.sandbox, runner.root)
	}
	if recorder.trigger != TriggerSchedule || recorder.engine != "traex" || recorder.model != "DeepSeek-V4-Pro" || recorder.input != runner.prompt {
		t.Fatalf("recorded start = trigger=%q engine=%q model=%q input_match=%v", recorder.trigger, recorder.engine, recorder.model, recorder.input == runner.prompt)
	}
	if recorder.successOutput != runner.result || recorder.failureDetail != "" {
		t.Fatalf("recorded finish output=%q failure=%q", recorder.successOutput, recorder.failureDetail)
	}
}

func TestWorkerFailsOnDependencyOrEmptyResult(t *testing.T) {
	base := Options{
		Runner: &fakeRunner{result: "ok"}, Recorder: &fakeRecorder{}, Prompts: fakePromptReader{text: "system"},
		SharedMemory: fakeMemoryReader{},
		Sandbox:      "danger-full-access", WorkspaceRoot: "/tmp/jarvis", Location: time.UTC,
		Engine: "traex", Model: "model",
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
	if detail := worker.recorder.(*fakeRecorder).failureDetail; !strings.Contains(detail, "empty final message") {
		t.Fatalf("recorded failure detail = %q", detail)
	}
}
