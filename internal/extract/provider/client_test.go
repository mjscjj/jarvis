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
)

func TestClientExtractUsesStrictSchema(t *testing.T) {
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
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		format := body["response_format"].(map[string]any)
		if format["type"] != "json_schema" || format["json_schema"].(map[string]any)["strict"] != true {
			t.Fatalf("response_format = %#v", format)
		}
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"candidates\":[]}","refusal":""}}]}`), nil
	})
	result, err := client.Extract(context.Background(), Prompt{System: "system", User: "user"})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("candidates = %#v", result.Candidates)
	}
}

func TestClientExtractFailsOnRefusal(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"","refusal":"cannot comply"}}]}`), nil
	})
	_, err = client.Extract(context.Background(), Prompt{System: "system", User: "user"})
	if !errors.Is(err, ErrModelRefusal) {
		t.Fatalf("Extract() error = %v", err)
	}
}

func TestClientExtractFailsOnInvalidCandidateJSON(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"{}","refusal":""}}]}`), nil
	})
	if _, err := client.Extract(context.Background(), Prompt{System: "system", User: "user"}); err == nil {
		t.Fatal("Extract() accepted missing candidates field")
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
