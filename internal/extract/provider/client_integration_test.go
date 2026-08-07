//go:build integration

package provider

import (
	"context"
	"os"
	"testing"
	"time"

	"jarvis/internal/ark"
	"jarvis/internal/config"
	"jarvis/internal/extract"
)

func TestClientLiveStructuredOutput(t *testing.T) {
	baseURL := ark.BaseURL
	apiKey := ark.APIKey
	model := os.Getenv("JARVIS_TEST_MODEL_NAME")
	if configPath := os.Getenv("JARVIS_TEST_MODEL_CONFIG"); configPath != "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			t.Fatalf("config.Load() error = %v", err)
		}
		model = cfg.Model.Model
	}
	if baseURL == "" || apiKey == "" || model == "" {
		t.Fatal("set JARVIS_TEST_MODEL_CONFIG or JARVIS_TEST_MODEL_NAME")
	}

	client, err := NewClient(baseURL, apiKey, model, 90*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	candidate := extract.Candidate{
		ActionType: "code_change", Status: "extracted", Title: "Refactor synthetic auth", Target: "synthetic/repo auth flow",
		Payload: "Refactor the synthetic auth flow in repo synthetic/repo and merge it.", SourceMessageIDs: []string{"om_synthetic"},
		SourceQuote: "Refactor synthetic auth",
	}
	same, err := client.SameAction(ctx, candidate, extract.SemanticTodo{
		ID: 1, ActionType: candidate.ActionType, Title: candidate.Title, Description: candidate.Payload,
		Target: candidate.Target, Status: "extracted", DedupFingerprint: "synthetic",
	})
	if err != nil {
		t.Fatalf("SameAction() error = %v", err)
	}
	if !same {
		t.Fatal("SameAction() = false for identical synthetic actions")
	}
}
