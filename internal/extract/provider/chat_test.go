package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"jarvis/internal/extract"
	"jarvis/internal/extract/tools"
)

// stubToolBox exposes one tool spec and records the tool calls it dispatches.
type stubToolBox struct {
	invoked []string
	result  json.RawMessage
	err     error
}

func (b *stubToolBox) Specs() []tools.Spec {
	return []tools.Spec{{
		Type: "function",
		Function: tools.SpecFunction{
			Name: "query_chat_history", Description: "d",
			Parameters: map[string]any{"type": "object"},
		},
	}}
}

func (b *stubToolBox) Invoke(_ context.Context, name string, _ json.RawMessage) (json.RawMessage, error) {
	b.invoked = append(b.invoked, name)
	if b.err != nil {
		return nil, b.err
	}
	return b.result, nil
}

func TestExtractWithToolsRunsToolThenReturnsFinal(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	round := 0
	sawTools := false
	sawToolResult := false
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := body["tools"]; ok {
			sawTools = true
		}
		round++
		if round == 1 {
			// First reply asks to call a tool.
			return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"query_chat_history","arguments":"{}"}}]}}]}`), nil
		}
		// Second request must contain the tool result message.
		messages := body["messages"].([]any)
		for _, m := range messages {
			msg := m.(map[string]any)
			if msg["role"] == "tool" && msg["tool_call_id"] == "call_1" {
				sawToolResult = true
			}
		}
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"candidates\":[]}","refusal":""}}]}`), nil
	})

	box := &stubToolBox{result: json.RawMessage(`{"count":0,"messages":[]}`)}
	result, err := client.ExtractWithTools(context.Background(), extract.Prompt{System: "s", User: "u"}, box)
	if err != nil {
		t.Fatalf("ExtractWithTools() error = %v", err)
	}
	if result == nil || len(result.Candidates) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if !sawTools || !sawToolResult || len(box.invoked) != 1 || box.invoked[0] != "query_chat_history" {
		t.Fatalf("tools=%v toolResult=%v invoked=%#v", sawTools, sawToolResult, box.invoked)
	}
}

func TestExtractWithToolsHasNoToolCallCountLimit(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	round := 0
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		round++
		if round <= 8 {
			return jsonResponse(http.StatusOK, fmt.Sprintf(`{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"call_%d","type":"function","function":{"name":"query_chat_history","arguments":"{}"}}]}}]}`, round)), nil
		}
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"candidates\":[]}","refusal":""}}]}`), nil
	})
	box := &stubToolBox{result: json.RawMessage(`{"ok":true}`)}
	if _, err := client.ExtractWithTools(context.Background(), extract.Prompt{System: "s", User: "u"}, box); err != nil {
		t.Fatalf("ExtractWithTools() stopped after repeated tool calls: %v", err)
	}
	if len(box.invoked) != 8 {
		t.Fatalf("tool calls = %d, want 8", len(box.invoked))
	}
}

func TestExtractWithToolsPropagatesToolError(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"query_chat_history","arguments":"{}"}}]}}]}`), nil
	})
	box := &stubToolBox{err: context.DeadlineExceeded}
	if _, err := client.ExtractWithTools(context.Background(), extract.Prompt{System: "s", User: "u"}, box); err == nil {
		t.Fatal("ExtractWithTools() swallowed tool error")
	}
}

func TestExtractWithToolsValidatesArgs(t *testing.T) {
	client, err := NewClient("https://model.test/v1", "plain-key", "model-name", time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	box := &stubToolBox{result: json.RawMessage(`{}`)}
	if _, err := client.ExtractWithTools(context.Background(), extract.Prompt{System: "", User: "u"}, box); err == nil {
		t.Fatal("ExtractWithTools() accepted blank system prompt")
	}
	if _, err := client.ExtractWithTools(context.Background(), extract.Prompt{System: "s", User: "u"}, nil); err == nil {
		t.Fatal("ExtractWithTools() accepted nil tool box")
	}
}
