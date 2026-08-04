package morningbrief

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	prompt, sandbox, root, stage string
	result                       string
	err                          error
	writeBrief                   bool
	workspaceRoot                string
	date                         string
}

func (f *fakeRunner) RunTextSandboxAtStage(_ context.Context, prompt, sandbox, root, stage string) (string, error) {
	f.prompt, f.sandbox, f.root, f.stage = prompt, sandbox, root, stage
	if f.writeBrief {
		dir := filepath.Join(f.workspaceRoot, "data", "morning-brief", f.date)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		path := filepath.Join(dir, "99-brief.md")
		if err := os.WriteFile(path, []byte("# 晨间作战简报 · "+f.date+"\n"), 0o644); err != nil {
			return "", err
		}
	}
	return f.result, f.err
}

type fakePromptReader struct {
	text string
	err  error
}

func (f fakePromptReader) Content(context.Context, string) (string, error) { return f.text, f.err }

func TestWorkerBuildsPromptAndUsesMorningBriefStage(t *testing.T) {
	root := t.TempDir()
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 8, 4, 8, 30, 0, 0, location)
	runner := &fakeRunner{
		result: "brief ready", writeBrief: true,
		workspaceRoot: root, date: "2026-08-04",
	}
	worker, err := NewWorker(Options{
		Runner: runner, Prompts: fakePromptReader{text: "morning mission"},
		Sandbox: "danger-full-access", WorkspaceRoot: root, Location: location,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return now }

	result, err := worker.Run(t.Context(), TriggerSchedule)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != runner.result {
		t.Fatalf("result = %q", result)
	}
	for _, want := range []string{
		"morning mission", "BEGIN_AVAILABLE_TOOLS", "BEGIN_MORNING_BRIEF",
		"2026-08-04T08:30:00+08:00", "自然日：2026-08-04", "trigger：schedule",
		"定时触发", "summarize-morning-brief",
	} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
	if runner.stage != AgentStage || runner.sandbox != "danger-full-access" || runner.root != root {
		t.Fatalf("runner args stage=%q sandbox=%q root=%q", runner.stage, runner.sandbox, runner.root)
	}
	if !worker.HasBriefFor(now) {
		t.Fatal("HasBriefFor should see the brief the run just wrote")
	}
	if worker.HasBriefFor(now.AddDate(0, 0, 1)) {
		t.Fatal("HasBriefFor should be per-day")
	}
}

// Which days count as workdays belongs to the cron spec, not to the worker: a
// Saturday run must go through so an explicitly scheduled or manual weekend
// brief is never silently dropped.
func TestWorkerHasNoWeekdayRuleOfItsOwn(t *testing.T) {
	root := t.TempDir()
	location := time.FixedZone("CST", 8*60*60)
	// 2026-08-08 is Saturday.
	saturday := time.Date(2026, 8, 8, 8, 30, 0, 0, location)
	for _, trigger := range []string{TriggerSchedule, TriggerManual} {
		runner := &fakeRunner{
			result: "weekend brief", writeBrief: true,
			workspaceRoot: root, date: "2026-08-08",
		}
		worker, err := NewWorker(Options{
			Runner: runner, Prompts: fakePromptReader{text: "system"},
			Sandbox: "danger-full-access", WorkspaceRoot: root, Location: location,
		})
		if err != nil {
			t.Fatal(err)
		}
		worker.now = func() time.Time { return saturday }
		result, err := worker.Run(t.Context(), trigger)
		if err != nil {
			t.Fatalf("Run(%s) error = %v", trigger, err)
		}
		if result != "weekend brief" {
			t.Fatalf("Run(%s) result = %q", trigger, result)
		}
	}
}

func TestWorkerPromptCarriesTriggerDeliveryPolicy(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{result: "manual brief", writeBrief: true, workspaceRoot: root}
	worker := mustWorker(t, root, runner)
	runner.date = worker.now().In(worker.location).Format("2006-01-02")
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !strings.Contains(runner.prompt, "投递策略：手动触发") || strings.Contains(runner.prompt, "投递策略：定时触发") {
		t.Fatalf("manual prompt delivery policy wrong:\n%s", runner.prompt)
	}
}

func TestWorkerFailsWhenArtifactMissingOrStale(t *testing.T) {
	root := t.TempDir()
	worker := mustWorker(t, root, &fakeRunner{result: "ok"})
	if _, err := worker.RunOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "morning brief artifact") {
		t.Fatalf("missing artifact error = %v", err)
	}

	date := worker.now().In(worker.location).Format("2006-01-02")
	dir := filepath.Join(root, "data", "morning-brief", date)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "99-brief.md")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	worker.runner = &fakeRunner{result: "ok"}
	if _, err := worker.RunOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "was not refreshed") {
		t.Fatalf("stale artifact error = %v", err)
	}
}

func TestWorkerRejectsUnknownTriggerAndEmptyResult(t *testing.T) {
	root := t.TempDir()
	worker := mustWorker(t, root, &fakeRunner{result: "ok"})
	if _, err := worker.Run(t.Context(), "cron"); err == nil || !strings.Contains(err.Error(), "trigger must be") {
		t.Fatalf("trigger error = %v", err)
	}
	worker.prompts = fakePromptReader{err: errors.New("missing")}
	if _, err := worker.RunOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "read morning brief system prompt") {
		t.Fatalf("prompt error = %v", err)
	}
	worker.prompts = fakePromptReader{text: "system"}
	worker.runner = &fakeRunner{result: "  "}
	if _, err := worker.RunOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "empty final message") {
		t.Fatalf("empty result error = %v", err)
	}
}

func TestNewWorkerRejectsMissingWorkspace(t *testing.T) {
	_, err := NewWorker(Options{
		Runner: &fakeRunner{result: "ok"}, Prompts: fakePromptReader{text: "system"},
		Sandbox: "danger-full-access", WorkspaceRoot: filepath.Join(t.TempDir(), "missing"),
		Location: time.UTC,
	})
	if err == nil || !strings.Contains(err.Error(), "stat morning brief workspace root") {
		t.Fatalf("workspace error = %v", err)
	}
}

func mustWorker(t *testing.T, root string, runner Runner) *Worker {
	t.Helper()
	worker, err := NewWorker(Options{
		Runner: runner, Prompts: fakePromptReader{text: "system"},
		Sandbox: "danger-full-access", WorkspaceRoot: root, Location: time.UTC,
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}
