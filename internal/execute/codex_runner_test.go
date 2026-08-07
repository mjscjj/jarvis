package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCodexRunnerPersistsAndResumesTaskSession(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	envPath := filepath.Join(dir, "task-id.txt")
	stagePath := filepath.Join(dir, "stage.txt")
	cwdPath := filepath.Join(dir, "cwd.txt")
	binPath := filepath.Join(dir, "fake-codex")
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "exec" ] && [ "${2:-}" = "resume" ] && [ "${3:-}" = "--help" ]; then
  printf '%s\n' '      --output-schema <FILE>'
  exit 0
fi
printf '%s\n' "$@" > "$FAKE_CODEX_ARGS"
printf '%s' "${JARVIS_TASK_ID:-}" > "$FAKE_CODEX_TASK_ID"
printf '%s' "${JARVIS_AGENT_STAGE:-}" > "$FAKE_CODEX_STAGE"
pwd > "$FAKE_CODEX_CWD"
output=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then output="$arg"; fi
  previous="$arg"
done
[ -n "$output" ]
printf '%s' '{"outcome":"completed","summary":"done","failure_reason":"","needs_followup":"","enrichments":[],"waiting":null}' > "$output"
printf '%s\n' '{"type":"thread.started","thread_id":"session-42"}'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":120,"cached_input_tokens":20,"output_tokens":30,"reasoning_output_tokens":10}}'
printf '%s\n' 'diagnostic stderr' >&2
`
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("FAKE_CODEX_ARGS", argsPath)
	t.Setenv("FAKE_CODEX_TASK_ID", envPath)
	t.Setenv("FAKE_CODEX_STAGE", stagePath)
	t.Setenv("FAKE_CODEX_CWD", cwdPath)
	t.Setenv("JARVIS_TASK_ID", "999")
	t.Setenv("JARVIS_AGENT_STAGE", "parent")

	runner, err := NewCodexRunner(binPath, "test-model", "medium", time.Minute)
	if err != nil {
		t.Fatalf("NewCodexRunner() error = %v", err)
	}
	stdoutPath := filepath.Join(dir, "stdout.jsonl")
	stderrPath := filepath.Join(dir, "stderr.log")
	for _, path := range []string{stdoutPath, stderrPath} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("initialize output capture %s: %v", path, err)
		}
	}
	first, err := runner.RunTaskWithOutput(
		t.Context(), "start", "danger-full-access", "", schemaExecution, 123,
		&codexOutputCapture{StdoutPath: stdoutPath, StderrPath: stderrPath},
	)
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if first.SessionID != "session-42" {
		t.Fatalf("session ID = %q", first.SessionID)
	}
	if !first.Usage.Reported || first.Usage.TotalTokens() != 150 {
		t.Fatalf("usage = %+v, want 150 total tokens", first.Usage)
	}
	args := readTestFile(t, argsPath)
	if strings.Contains(args, "--ephemeral") {
		t.Fatalf("persisted Task run contains --ephemeral:\n%s", args)
	}
	if !strings.Contains(args, "--skip-git-repo-check") || strings.Contains(args, "--cd\n") {
		t.Fatalf("Task without an explicit repo changed its working directory:\n%s", args)
	}
	inheritedCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get inherited cwd: %v", err)
	}
	inheritedCWD, err = filepath.EvalSymlinks(inheritedCWD)
	if err != nil {
		t.Fatalf("resolve inherited cwd: %v", err)
	}
	if got := strings.TrimSpace(readTestFile(t, cwdPath)); got != inheritedCWD {
		t.Fatalf("Task cwd = %q, want inherited cwd %q", got, inheritedCWD)
	}
	if got := readTestFile(t, envPath); got != "123" {
		t.Fatalf("JARVIS_TASK_ID = %q, want 123", got)
	}
	if got := readTestFile(t, stagePath); got != "execute" {
		t.Fatalf("JARVIS_AGENT_STAGE = %q, want execute", got)
	}
	if got := readTestFile(t, stdoutPath); !strings.Contains(got, `"type":"thread.started"`) {
		t.Fatalf("captured stdout missing thread event: %s", got)
	}
	if got := readTestFile(t, stderrPath); !strings.Contains(got, "diagnostic stderr") {
		t.Fatalf("captured stderr missing diagnostic: %s", got)
	}

	resumed, err := runner.ResumeTask(t.Context(), "session-42", "continue", "danger-full-access", "", schemaExecution, 123)
	if err != nil {
		t.Fatalf("ResumeTask() error = %v", err)
	}
	if resumed.SessionID != "session-42" {
		t.Fatalf("resumed session ID = %q", resumed.SessionID)
	}
	args = readTestFile(t, argsPath)
	for _, want := range []string{"exec\nresume\nsession-42\n", "sandbox_mode=\"danger-full-access\"", "--output-schema\n"} {
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

	if _, err := runner.RunTextSandboxAt(
		t.Context(),
		"build daily panorama",
		"danger-full-access",
		dir,
	); err != nil {
		t.Fatalf("RunTextSandboxAt() error = %v", err)
	}
	args = readTestFile(t, argsPath)
	if !strings.Contains(args, "--cd\n"+dir+"\n") {
		t.Fatalf("workspace-rooted text run args missing --cd %q:\n%s", dir, args)
	}
	wantCWD, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve expected cwd: %v", err)
	}
	if got := strings.TrimSpace(readTestFile(t, cwdPath)); got != wantCWD {
		t.Fatalf("workspace-rooted text run cwd = %q, want %q", got, wantCWD)
	}

	if _, err := runner.RunTextSandboxAtStage(
		t.Context(), "review world", "danger-full-access", dir, "proactive",
	); err != nil {
		t.Fatalf("RunTextSandboxAtStage() error = %v", err)
	}
	if got := readTestFile(t, stagePath); got != "proactive" {
		t.Fatalf("stage-aware JARVIS_AGENT_STAGE = %q, want proactive", got)
	}
	if _, err := runner.RunTextSandboxAtStage(
		t.Context(), "review world", "danger-full-access", dir, "../bad",
	); err == nil {
		t.Fatal("RunTextSandboxAtStage accepted an invalid stage")
	}
}

func TestCodexRunnerInterruptKillsRunningProcess(t *testing.T) {
	dir := t.TempDir()
	startedPath := filepath.Join(dir, "started")
	binPath := filepath.Join(dir, "fake-codex")
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "exec" ] && [ "${2:-}" = "resume" ] && [ "${3:-}" = "--help" ]; then
  printf '%s\n' '      --output-schema <FILE>'
  exit 0
fi
touch "$FAKE_CODEX_STARTED"
sleep 30
`
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("FAKE_CODEX_STARTED", startedPath)
	runner, err := NewCodexRunner(binPath, "test-model", "medium", time.Minute)
	if err != nil {
		t.Fatalf("NewCodexRunner() error = %v", err)
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := runner.RunTask(ctx, "start", "danger-full-access", "", schemaExecution, 123)
		result <- err
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(startedPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake codex did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel(ErrExecutionInterrupted)
	select {
	case err := <-result:
		if !errors.Is(err, ErrExecutionInterrupted) {
			t.Fatalf("RunTask() error = %v, want ErrExecutionInterrupted", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunTask() did not stop after interrupt")
	}
}

// writeSchemalessResumeCLI fakes a traecli-style agent CLI: `exec resume` does
// not advertise --output-schema, and the final message it writes is chosen per
// invocation by the caller-supplied shell case body.
func writeSchemalessResumeCLI(t *testing.T, dir, lastMessageCases string) string {
	t.Helper()
	binPath := filepath.Join(dir, "fake-traex")
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "exec" ] && [ "${2:-}" = "resume" ] && [ "${3:-}" = "--help" ]; then
  printf '%s\n' '  -o, --output-last-message <FILE>'
  exit 0
fi
printf '%s\n' "$@" >> "$FAKE_ARGS"
cat >> "$FAKE_PROMPT"
output=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then output="$arg"; fi
  previous="$arg"
done
[ -n "$output" ]
count=$(cat "$FAKE_COUNT" 2>/dev/null || echo 0)
count=$((count + 1))
printf '%s' "$count" > "$FAKE_COUNT"
case "$count" in
` + lastMessageCases + `
esac
printf '%s\n' '{"type":"thread.started","thread_id":"session-7"}'
`
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake traex: %v", err)
	}
	t.Setenv("FAKE_ARGS", filepath.Join(dir, "args.txt"))
	t.Setenv("FAKE_PROMPT", filepath.Join(dir, "prompt.txt"))
	t.Setenv("FAKE_COUNT", filepath.Join(dir, "count.txt"))
	return binPath
}

const validExecutionResult = `{"outcome":"completed","summary":"done","failure_reason":"","needs_followup":"","enrichments":[],"effects":[],"waiting":null}`

func TestCodexRunnerResumeRewritesInvalidResultWhenSchemaFlagUnsupported(t *testing.T) {
	dir := t.TempDir()
	binPath := writeSchemalessResumeCLI(t, dir, `  1) printf '%s' 'sure, here you go: {"outcome":"completed"}' > "$output" ;;
  *) printf '%s' '`+validExecutionResult+`' > "$output" ;;`)

	runner, err := NewCodexRunner(binPath, "test-model", "medium", time.Minute)
	if err != nil {
		t.Fatalf("NewCodexRunner() error = %v", err)
	}
	if runner.resumeOutputSchema {
		t.Fatal("probe reported --output-schema support for a CLI that does not advertise it")
	}

	resumed, err := runner.ResumeTask(
		t.Context(), "session-7", "continue", "danger-full-access", "", schemaExecution, 123,
	)
	if err != nil {
		t.Fatalf("ResumeTask() error = %v", err)
	}
	if resumed.Result == nil || resumed.Result.Outcome != "completed" {
		t.Fatalf("resumed result = %+v, want outcome=completed", resumed.Result)
	}
	if got := readTestFile(t, filepath.Join(dir, "count.txt")); got != "2" {
		t.Fatalf("invocation count = %q, want 2 (one bad turn plus one rewrite)", got)
	}
	args := readTestFile(t, filepath.Join(dir, "args.txt"))
	if strings.Contains(args, "--output-schema") {
		t.Fatalf("resume passed --output-schema to a CLI that rejects it:\n%s", args)
	}
	prompt := readTestFile(t, filepath.Join(dir, "prompt.txt"))
	if strings.Count(prompt, "BEGIN_FINAL_MESSAGE_CONTRACT") != 2 {
		t.Fatalf("both turns must carry the inline contract:\n%s", prompt)
	}
	if !strings.Contains(prompt, "不符合要求的返回格式") {
		t.Fatalf("rewrite turn does not report the violation:\n%s", prompt)
	}
}

// TestCodexRunnerRewritesInvalidResultOnFreshRun pins that --output-schema is
// treated as guidance, not enforcement: a first run whose work already produced
// real side effects must be asked to restate its verdict in the same session,
// never re-run, and never reported as a failed Task over a malformed report.
func TestCodexRunnerRewritesInvalidResultOnFreshRun(t *testing.T) {
	dir := t.TempDir()
	binPath := writeSchemalessResumeCLI(t, dir, `  1) printf '%s' '我已经把消息发出去了。' > "$output" ;;
  *) printf '%s' '`+validExecutionResult+`' > "$output" ;;`)

	runner, err := NewCodexRunner(binPath, "test-model", "medium", time.Minute)
	if err != nil {
		t.Fatalf("NewCodexRunner() error = %v", err)
	}
	run, err := runner.RunTask(t.Context(), "do the work", "danger-full-access", "", schemaExecution, 123)
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if run.Result == nil || run.Result.Outcome != "completed" {
		t.Fatalf("result = %+v, want outcome=completed", run.Result)
	}
	if got := readTestFile(t, filepath.Join(dir, "count.txt")); got != "2" {
		t.Fatalf("invocation count = %q, want 2 (one bad run plus one rewrite)", got)
	}
	args := readTestFile(t, filepath.Join(dir, "args.txt"))
	if !strings.Contains(args, "resume") {
		t.Fatalf("rewrite must resume the same session rather than re-run the Task:\n%s", args)
	}
	prompt := readTestFile(t, filepath.Join(dir, "prompt.txt"))
	if !strings.Contains(prompt, "不要重做上一轮已经完成的工作") {
		t.Fatalf("rewrite turn must forbid redoing the work:\n%s", prompt)
	}
}

func TestCodexRunnerResumeFailsAfterRewriteBudgetExhausted(t *testing.T) {
	dir := t.TempDir()
	binPath := writeSchemalessResumeCLI(t, dir, `  *) printf '%s' 'still not JSON' > "$output" ;;`)

	runner, err := NewCodexRunner(binPath, "test-model", "medium", time.Minute)
	if err != nil {
		t.Fatalf("NewCodexRunner() error = %v", err)
	}
	_, err = runner.ResumeTask(
		t.Context(), "session-7", "continue", "danger-full-access", "", schemaExecution, 123,
	)
	if !errors.Is(err, ErrSchemaViolation) {
		t.Fatalf("ResumeTask() error = %v, want ErrSchemaViolation", err)
	}
	want := strconv.Itoa(maxResumeSchemaRewrites + 1)
	if got := readTestFile(t, filepath.Join(dir, "count.txt")); got != want {
		t.Fatalf("invocation count = %q, want %s", got, want)
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
