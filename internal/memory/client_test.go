package memory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestClientAdd(t *testing.T) {
	client, err := NewClient("http://sidecar.test", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/memories" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var input AddInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !input.Infer || input.Transcript != "hello" {
			t.Fatalf("input = %#v", input)
		}
		return jsonResponse(http.StatusOK, `{"results":[]}`), nil
	})
	if err := client.Add(context.Background(), AddInput{Transcript: "hello", Infer: true}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
}

func TestClientFailsOnSidecarError(t *testing.T) {
	client, err := NewClient("http://sidecar.test", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusServiceUnavailable, `{"detail":"qdrant unavailable"}`), nil
	})
	err = client.Add(context.Background(), AddInput{Transcript: "hello", Infer: true})
	if err == nil || !strings.Contains(err.Error(), "status=503") {
		t.Fatalf("Add() error = %v", err)
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

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient("127.0.0.1:18900", time.Second); err == nil {
		t.Fatal("NewClient() accepted relative URL")
	}
	if _, err := NewClient("http://127.0.0.1:18900", 0); err == nil {
		t.Fatal("NewClient() accepted zero timeout")
	}
}
