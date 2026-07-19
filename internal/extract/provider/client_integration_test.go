package provider

import (
	"context"
	"os"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/extract"
)

func TestClientLiveStructuredOutput(t *testing.T) {
	baseURL := os.Getenv("JARVIS_TEST_MODEL_BASE_URL")
	apiKey := os.Getenv("JARVIS_TEST_MODEL_API_KEY")
	model := os.Getenv("JARVIS_TEST_MODEL_NAME")
	if configPath := os.Getenv("JARVIS_TEST_MODEL_CONFIG"); configPath != "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		baseURL = cfg.Model.BaseURL
		apiKey = cfg.Model.APIKey
		model = cfg.Model.Model
	}
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("set JARVIS_TEST_MODEL_CONFIG or all JARVIS_TEST_MODEL_* variables")
	}

	client, err := NewClient(baseURL, apiKey, model, 90*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := client.Extract(ctx, extract.Prompt{
		System: "Extract work todo candidates. The user message below explicitly contains no work or action item, so return an empty candidates array.",
		User:   "Connectivity check only. There is no task, request, commitment, or follow-up in this message.",
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("Extract() candidates = %#v, want empty", result.Candidates)
	}
	candidate := extract.Candidate{
		ActionType: "code_change", Title: "Refactor synthetic auth", Description: "Refactor the synthetic auth flow",
		CommitmentStrength: "firm", SourceMessageIDs: []string{"om_synthetic"},
		SourceQuote: "Refactor synthetic auth", InfoSufficient: true,
		Slots: map[string]any{"repo_ref": "synthetic/repo", "change_summary": "refactor auth flow"},
	}
	same, err := client.SameAction(ctx, candidate, extract.SemanticTodo{
		ID: 1, ActionType: candidate.ActionType, Title: candidate.Title, Description: candidate.Description,
		Slots: candidate.Slots, Status: "extracted", DedupFingerprint: "synthetic",
	})
	if err != nil {
		t.Fatalf("SameAction() error = %v", err)
	}
	if !same {
		t.Fatal("SameAction() = false for identical synthetic actions")
	}
}
