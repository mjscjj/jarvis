package execute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexRunnerPersistsAndResumesTaskSession(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	envPath := filepath.Join(dir, "task-id.txt")
	binPath := filepath.Join(dir, "fake-codex")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$FAKE_CODEX_ARGS"
printf '%s' "${JARVIS_TASK_ID:-}" > "$FAKE_CODEX_TASK_ID"
output=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then output="$arg"; fi
  previous="$arg"
done
[ -n "$output" ]
printf '%s' '{"outcome":"completed","summary":"done","failure_reason":"","needs_followup":"","enrichments":[],"waiting":null}' > "$output"
printf '%s\n' '{"type":"thread.started","thread_id":"session-42"}'
`
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("FAKE_CODEX_ARGS", argsPath)
	t.Setenv("FAKE_CODEX_TASK_ID", envPath)

	runner, err := NewCodexRunner(binPath, "test-model", "medium", time.Minute)
	if err != nil {
		t.Fatalf("NewCodexRunner() error = %v", err)
	}
	first, err := runner.RunTask(t.Context(), "start", "danger-full-access", "", schemaExecution, 123)
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if first.SessionID != "session-42" {
		t.Fatalf("session ID = %q", first.SessionID)
	}
	args := readTestFile(t, argsPath)
	if strings.Contains(args, "--ephemeral") {
		t.Fatalf("persisted Task run contains --ephemeral:\n%s", args)
	}
	if got := readTestFile(t, envPath); got != "123" {
		t.Fatalf("JARVIS_TASK_ID = %q, want 123", got)
	}

	resumed, err := runner.ResumeTask(t.Context(), "session-42", "continue", "danger-full-access", "", schemaExecution, 123)
	if err != nil {
		t.Fatalf("ResumeTask() error = %v", err)
	}
	if resumed.SessionID != "session-42" {
		t.Fatalf("resumed session ID = %q", resumed.SessionID)
	}
	args = readTestFile(t, argsPath)
	for _, want := range []string{"exec\nresume\nsession-42\n", "sandbox_mode=\"danger-full-access\""} {
		if !strings.Contains(args, want) {
			t.Fatalf("resume args missing %q:\n%s", want, args)
		}
	}

	if _, err := runner.RunText(t.Context(), "summarize"); err != nil {
		t.Fatalf("RunText() error = %v", err)
	}
	args = readTestFile(t, argsPath)
	if !strings.Contains(args, "--ephemeral") {
		t.Fatalf("one-shot RunText args do not contain --ephemeral:\n%s", args)
	}
	if got := readTestFile(t, envPath); got != "" {
		t.Fatalf("one-shot RunText JARVIS_TASK_ID = %q, want empty", got)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
