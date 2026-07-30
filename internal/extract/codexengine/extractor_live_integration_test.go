//go:build integration

package codexengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/extract"
)

// TestExtractorLiveStructuredOutput runs one real M3 extraction through the
// configured agent CLI and model, using the production system prompt. M3 has the
// largest of the enforced schemas, so this is the check that the configured
// model can still satisfy it; every unit test stubs the CLI out.
func TestExtractorLiveStructuredOutput(t *testing.T) {
	configPath := os.Getenv("JARVIS_TEST_EXTRACT_CODEX_CONFIG")
	if configPath == "" {
		t.Fatal("set JARVIS_TEST_EXTRACT_CODEX_CONFIG to a Jarvis config file")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	model := cfg.Codex.Model
	if override := os.Getenv("JARVIS_TEST_EXTRACT_CODEX_MODEL"); override != "" {
		model = override
	}
	systemPrompt, err := os.ReadFile(
		filepath.Join(filepath.Dir(configPath), "prompts", "m3-system-prompt.md"),
	)
	if err != nil {
		t.Fatalf("read M3 system prompt: %v", err)
	}

	extractor, err := New(Options{
		Bin: cfg.Codex.Bin, Model: model, Sandbox: cfg.Extract.CodexSandbox,
		Network: cfg.Extract.CodexNetwork, ReasoningEffort: cfg.Extract.CodexReasoningEffort,
		Timeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Logf("bin=%s model=%s sandbox=%s effort=%s",
		cfg.Codex.Bin, model, cfg.Extract.CodexSandbox, cfg.Extract.CodexReasoningEffort)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := extractor.ExtractWithTools(ctx, extract.Prompt{
		System: string(systemPrompt),
		User: `这是格式连通性自检。不要调用任何工具，只根据下面的消息给出抽取结果。

BEGIN_MESSAGES
[new] message_id=om_live_probe chat="Jarvis 研发群" sender="张伟（我的 leader）" time="2026-07-30T21:00:00+08:00"
文本：小明你把 jarvis 的 README 里那个错别字改一下，今天下班前提个 MR。
END_MESSAGES`,
	}, nil)
	if err != nil {
		t.Fatalf("ExtractWithTools() error = %v", err)
	}
	if result == nil {
		t.Fatal("ExtractWithTools() returned a nil result")
	}
	t.Logf("candidates=%d", len(result.Candidates))
	for index, candidate := range result.Candidates {
		t.Logf("candidate[%d] action_type=%s title=%q source_quote=%q",
			index, candidate.ActionType, candidate.Title, candidate.SourceQuote)
	}
}
