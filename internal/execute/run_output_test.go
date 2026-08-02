package execute

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestPrepareTaskRunOutputPublishesCompleteBundle(t *testing.T) {
	executor := &AgentExecutor{runsDir: t.TempDir()}
	startedAt := time.Date(2026, 7, 23, 21, 0, 0, 0, time.UTC)
	capture, err := executor.prepareTaskRunOutput(67, "execute", startedAt, "FULL TASK INPUT")
	if err != nil {
		t.Fatalf("prepareTaskRunOutput() error = %v", err)
	}
	if err := os.WriteFile(capture.StdoutPath, []byte(`{"type":"thread.started"}`), 0o600); err != nil {
		t.Fatalf("write stdout fixture: %v", err)
	}
	if err := os.WriteFile(capture.StderrPath, []byte("warning"), 0o600); err != nil {
		t.Fatalf("write stderr fixture: %v", err)
	}
	paths, err := latestTaskRunOutputPaths(executor.runsDir, 67)
	if err != nil {
		t.Fatalf("latestTaskRunOutputPaths() error = %v", err)
	}
	if paths == nil || paths.Stage != "execute" {
		t.Fatalf("paths = %#v", paths)
	}
	for path, want := range map[string]string{
		paths.Prompt: "FULL TASK INPUT",
		paths.Stdout: `"type":"thread.started"`,
		paths.Stderr: "warning",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(content), want) {
			t.Fatalf("%s missing %q: %s", path, want, content)
		}
	}
}

func TestLatestTaskRunOutputPathsChoosesNewestInvocation(t *testing.T) {
	executor := &AgentExecutor{runsDir: t.TempDir()}
	first := time.Unix(0, 100).UTC()
	second := time.Unix(0, 200).UTC()
	if _, err := executor.prepareTaskRunOutput(9, "execute", first, "first"); err != nil {
		t.Fatalf("prepare first output: %v", err)
	}
	if _, err := executor.prepareTaskRunOutput(9, "apply", second, "second"); err != nil {
		t.Fatalf("prepare second output: %v", err)
	}
	paths, err := latestTaskRunOutputPaths(executor.runsDir, 9)
	if err != nil {
		t.Fatalf("latestTaskRunOutputPaths() error = %v", err)
	}
	if paths == nil || paths.Stage != "apply" {
		t.Fatalf("latest paths = %#v, want apply", paths)
	}
	content, err := os.ReadFile(paths.Prompt)
	if err != nil {
		t.Fatalf("read latest prompt: %v", err)
	}
	if string(content) != "second" {
		t.Fatalf("latest prompt = %q, want second", content)
	}
}
