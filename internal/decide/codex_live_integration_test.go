//go:build integration

package decide

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
)

// TestCodexDeciderLiveDecision runs one real M4 decision through the configured
// agent CLI and model, using the production system prompt. It is the only check
// that the configured model actually honours the disposition/plan/payload
// contract; the unit tests all stub the CLI out.
func TestCodexDeciderLiveDecision(t *testing.T) {
	configPath := os.Getenv("JARVIS_TEST_DECIDE_CODEX_CONFIG")
	if configPath == "" {
		t.Fatal("set JARVIS_TEST_DECIDE_CODEX_CONFIG to a Jarvis config file")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	model := cfg.Codex.Model
	if override := os.Getenv("JARVIS_TEST_DECIDE_CODEX_MODEL"); override != "" {
		model = override
	}
	systemPrompt, err := os.ReadFile(
		filepath.Join(filepath.Dir(configPath), "prompts", "m4-system-prompt.md"),
	)
	if err != nil {
		t.Fatalf("read M4 system prompt: %v", err)
	}

	decider, err := NewCodexDecider(CodexOptions{
		Bin: cfg.Codex.Bin, Model: model, Timeout: 5 * time.Minute,
		Sandbox: cfg.Decide.CodexSandbox, Network: cfg.Decide.CodexNetwork,
		ReasoningEffort: cfg.Decide.CodexReasoningEffort,
	})
	if err != nil {
		t.Fatalf("NewCodexDecider() error = %v", err)
	}
	t.Logf("bin=%s model=%s sandbox=%s effort=%s",
		cfg.Codex.Bin, model, cfg.Decide.CodexSandbox, cfg.Decide.CodexReasoningEffort)

	prompt, err := BuildCodexPrompt(CodexPromptInput{
		Todo: &domain.Todo{
			ID: 424242, Title: "修正 README 错别字", ActionType: "code_change",
			Target: "jarvis README", Status: "extracted",
			ExtractionResult: []byte(`{
              "action_type":"code_change",
              "title":"修正 README 错别字",
              "target":"jarvis README",
              "desired_outcome":"README 中的错别字被改正并提交",
              "description":"leader 在群里提到 jarvis 的 README 有个错别字，让我抽空改掉",
              "context":"未指明是哪一处错别字，也未指明具体文件路径",
              "open_questions":["具体是哪一处错别字"],
              "commitment_strength":"soft",
              "source_message_ids":["om_live_probe"],
              "source_quote":"README 里有个错别字，你抽空改下"
            }`),
		},
		RuleScore:    RuleScore{Confidence: 0.6, Risk: 0.2},
		Background:   []byte(`{"principal":{"name":"我"},"project":{"name":"jarvis"}}`),
		SystemPrompt: string(systemPrompt),
	})
	if err != nil {
		t.Fatalf("BuildCodexPrompt() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := decider.Decide(ctx, CodexInput{Prompt: prompt.Text})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	switch result.Decision.Disposition {
	case DispositionReady, DispositionNeedReview, DispositionNeedInfo, DispositionDrop:
	default:
		t.Fatalf("Decide() disposition = %q, not a valid disposition", result.Decision.Disposition)
	}
	if len(result.Decision.Payload) == 0 {
		t.Fatal("Decide() returned an empty payload")
	}
	t.Logf("disposition=%s session=%s", result.Decision.Disposition, result.SessionID)
	t.Logf("plan=%s", string(result.Decision.Plan))
	t.Logf("payload=%s", string(result.Decision.Payload))
}
