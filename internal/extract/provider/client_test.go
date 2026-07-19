package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"jarvis/internal/extract"
)

// completeStructured (shared by SameAction and ExtractWithTools) is exercised
// end to end below: header/auth, strict json_schema, and unset temperature are
// all asserted through SameAction, so no separate Extract test is needed.
func TestClientCompleteStructuredSetsHeadersAndStrictSchema(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer plain-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := request.Header.Get("User-Agent"); got != "jarvis/0.1" {
			t.Fatalf("User-Agent = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		format := body["response_format"].(map[string]any)
		if format["type"] != "json_schema" || format["json_schema"].(map[string]any)["strict"] != true {
			t.Fatalf("response_format = %#v", format)
		}
		if _, ok := body["temperature"]; ok {
			t.Fatalf("request must leave provider temperature unset: %#v", body)
		}
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"same_action\":true}","refusal":""}}]}`), nil
	})
	if _, err := client.SameAction(context.Background(), providerCandidate(), extract.SemanticTodo{
		ID: 7, ActionType: "code_change", Title: "修改鉴权", Description: "修改鉴权逻辑",
		Slots: map[string]any{"repo_ref": "jarvis", "change_summary": "修改鉴权"}, Status: "extracted",
	}); err != nil {
		t.Fatalf("SameAction() error = %v", err)
	}
}

func TestClientSameActionFailsOnRefusal(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"","refusal":"cannot comply"}}]}`), nil
	})
	_, err = client.SameAction(context.Background(), providerCandidate(), extract.SemanticTodo{
		ID: 7, ActionType: "code_change", Title: "修改鉴权", Description: "修改鉴权逻辑",
	})
	if !errors.Is(err, ErrModelRefusal) {
		t.Fatalf("SameAction() error = %v, want ErrModelRefusal", err)
	}
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient("", "key", "model", time.Second); err == nil {
		t.Fatal("NewClient() accepted empty base URL")
	}
	if _, err := NewClient("https://model.test/v1", "", "model", time.Second); err == nil {
		t.Fatal("NewClient() accepted empty API key")
	}
}

func TestClientSameActionUsesStrictBooleanSchema(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		format := body["response_format"].(map[string]any)["json_schema"].(map[string]any)
		if format["name"] != "todo_same_action" || format["strict"] != true {
			t.Fatalf("response_format = %#v", format)
		}
		schema := format["schema"].(map[string]any)
		if schema["additionalProperties"] != false {
			t.Fatalf("schema = %#v", schema)
		}
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"same_action\":true}","refusal":""}}]}`), nil
	})
	same, err := client.SameAction(context.Background(), providerCandidate(), extract.SemanticTodo{
		ID: 7, ActionType: "code_change", Title: "修改鉴权", Description: "修改鉴权逻辑",
		Slots: map[string]any{"repo_ref": "jarvis", "change_summary": "修改鉴权"}, Status: "extracted",
	})
	if err != nil {
		t.Fatalf("SameAction() error = %v", err)
	}
	if !same {
		t.Fatal("SameAction() = false, want true")
	}
}

func TestClientSameActionRejectsNonBooleanResult(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"same_action\":\"yes\"}","refusal":""}}]}`), nil
	})
	_, err = client.SameAction(context.Background(), providerCandidate(), extract.SemanticTodo{
		ID: 7, ActionType: "code_change", Title: "修改鉴权", Description: "修改鉴权逻辑",
	})
	if err == nil {
		t.Fatal("SameAction() accepted non-boolean result")
	}
}

func providerCandidate() extract.Candidate {
	return extract.Candidate{
		ActionType: "code_change", Title: "修改鉴权", Description: "修改鉴权逻辑",
		CommitmentStrength: "firm", SourceMessageIDs: []string{"om_1"}, SourceQuote: "修改鉴权",
		Slots: map[string]any{"repo_ref": "jarvis", "change_summary": "修改鉴权"}, InfoSufficient: true,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
