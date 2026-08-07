package embedding

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"jarvis/internal/agentusage"
	"jarvis/internal/ark"
)

func TestNewClientUsesCodeFixedArkConfig(t *testing.T) {
	client, err := NewClient(time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.baseURL != ark.BaseURL || client.apiKey != ark.APIKey || client.model != ark.EmbeddingModel || client.dimensions != ark.EmbeddingDimensions {
		t.Fatalf("client config = base=%q key=%q model=%q dimensions=%d", client.baseURL, client.apiKey, client.model, client.dimensions)
	}
}

func TestClientEmbed(t *testing.T) {
	client, err := newClient("https://model.test/v1", "plain-key", "embed-model", 3, time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/embeddings/multimodal" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer plain-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		input, ok := body["input"].([]any)
		if !ok || len(input) != 1 || input[0].(map[string]any)["type"] != "text" || input[0].(map[string]any)["text"] != "todo text" {
			t.Fatalf("request input = %#v", body["input"])
		}
		if body["model"] != "embed-model" || body["encoding_format"] != "float" || body["dimensions"] != float64(3) {
			t.Fatalf("request body = %#v", body)
		}
		return jsonResponse(http.StatusOK, `{"data":{"object":"embedding","embedding":[0.1,0.2,0.3]},"usage":{"prompt_tokens":8,"total_tokens":8}}`), nil
	})
	ctx, collector := agentusage.WithCollector(context.Background())
	vector, err := client.Embed(ctx, "todo text")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vector) != 3 || vector[0] != float32(0.1) || vector[2] != float32(0.3) {
		t.Fatalf("vector = %#v", vector)
	}
	if usage := collector.Total(); !usage.Reported || usage.TotalTokens() != 8 {
		t.Fatalf("usage = %+v, want 8 input tokens", usage)
	}
}

func TestClientEmbedRejectsDimensionMismatch(t *testing.T) {
	client, err := newClient("https://model.test/v1", "plain-key", "embed-model", 3, time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"data":{"embedding":[0.1]}}`), nil
	})
	if _, err := client.Embed(context.Background(), "todo text"); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("Embed() error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}
}
