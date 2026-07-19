package provider

import (
	"context"
	"os"
	"testing"
	"time"

	"jarvis/internal/extract"
)

func TestClientLiveStructuredOutput(t *testing.T) {
	baseURL := os.Getenv("JARVIS_TEST_MODEL_BASE_URL")
	apiKey := os.Getenv("JARVIS_TEST_MODEL_API_KEY")
	model := os.Getenv("JARVIS_TEST_MODEL_NAME")
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("set JARVIS_TEST_MODEL_BASE_URL, JARVIS_TEST_MODEL_API_KEY and JARVIS_TEST_MODEL_NAME")
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
}
