//go:build integration

package execute

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"jarvis/internal/config"
)

// TestCodexRunnerLiveResumeSchemaContract exercises the real agent CLI end to
// end: a fresh persisted Task turn, then a resume turn on the same session.
// Both must return a final message that satisfies executionResultSchema. It is
// the only check that covers the resume path on a CLI that rejects
// --output-schema (traecli), where the contract lives in the prompt and the
// parser is the sole gate.
func TestCodexRunnerLiveResumeSchemaContract(t *testing.T) {
	configPath := os.Getenv("JARVIS_TEST_EXECUTE_CONFIG")
	if configPath == "" {
		t.Fatal("set JARVIS_TEST_EXECUTE_CONFIG to a Jarvis config file")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	// Overrides let the same live check run against either CLI, so the
	// --output-schema branch kept for official codex stays covered too.
	bin := cfg.Execute.Bin
	if override := os.Getenv("JARVIS_TEST_EXECUTE_BIN"); override != "" {
		bin = override
	}
	model := cfg.Execute.Model
	if override := os.Getenv("JARVIS_TEST_EXECUTE_MODEL"); override != "" {
		model = override
	}

	runner, err := NewCodexRunner(bin, model, cfg.Execute.ReasoningEffort, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewCodexRunner() error = %v", err)
	}
	t.Logf("bin=%s model=%s resume_output_schema=%v", bin, model, runner.resumeOutputSchema)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	const taskID = 999999
	first, err := runner.RunTask(ctx, livePrompt("live probe ok"), "read-only", "", schemaExecution, taskID)
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if first.SessionID == "" {
		t.Fatal("RunTask() returned an empty session ID; resume would be impossible")
	}
	if first.Result == nil || first.Result.Outcome != "completed" {
		t.Fatalf("RunTask() result = %+v, want outcome=completed", first.Result)
	}
	t.Logf("fresh turn ok: session=%s summary=%q", first.SessionID, first.Result.Summary)

	resumed, err := runner.ResumeTask(
		ctx, first.SessionID, livePrompt("live resume ok"), "read-only", "", schemaExecution, taskID,
	)
	if err != nil {
		t.Fatalf("ResumeTask() error = %v", err)
	}
	if resumed.Result == nil || resumed.Result.Outcome != "completed" {
		t.Fatalf("ResumeTask() result = %+v, want outcome=completed", resumed.Result)
	}
	if strings.TrimSpace(resumed.Result.Summary) == "" {
		t.Fatal("ResumeTask() returned a blank summary")
	}
	t.Logf("resume turn ok: summary=%q", resumed.Result.Summary)
}

func livePrompt(summary string) string {
	return `这是一次格式连通性自检，不是真实任务。

不要调用任何工具，不要读写任何文件，不要联网。直接给出最终消息：
outcome 填 "completed"，summary 填 "` + summary + `"，
failure_reason 与 needs_followup 填空字符串，enrichments 与 effects 填空数组，waiting 填 null。`
}
