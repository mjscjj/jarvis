//go:build integration

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
		t.Fatal("set JARVIS_TEST_MODEL_CONFIG or all JARVIS_TEST_MODEL_* variables")
	}

	client, err := NewClient(baseURL, apiKey, model, 90*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	candidate := extract.Candidate{
		ActionType: "code_change", Title: "Refactor synthetic auth", Target: "synthetic/repo auth flow",
		Description: "Refactor the synthetic auth flow", Context: "repo synthetic/repo",
		OpenQuestions:      []string{},
		CommitmentStrength: "firm", SourceMessageIDs: []string{"om_synthetic"},
		SourceQuote: "Refactor synthetic auth",
	}
	same, err := client.SameAction(ctx, candidate, extract.SemanticTodo{
		ID: 1, ActionType: candidate.ActionType, Title: candidate.Title, Description: candidate.Description,
		Target: candidate.Target, Status: "extracted", DedupFingerprint: "synthetic",
	})
	if err != nil {
		t.Fatalf("SameAction() error = %v", err)
	}
	if !same {
		t.Fatal("SameAction() = false for identical synthetic actions")
	}
}
