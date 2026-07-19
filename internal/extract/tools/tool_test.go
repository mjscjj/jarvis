package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type stubTool struct {
	name   string
	result json.RawMessage
	err    error
	calls  int
}

func (s *stubTool) Name() string           { return s.name }
func (s *stubTool) Description() string    { return "stub" }
func (s *stubTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (s *stubTool) Invoke(context.Context, json.RawMessage) (json.RawMessage, error) {
	s.calls++
	return s.result, s.err
}

func TestNewRegistryRejectsDuplicateAndBlank(t *testing.T) {
	if _, err := NewRegistry(&stubTool{name: "a", result: json.RawMessage("{}")}, &stubTool{name: "a"}); err == nil {
		t.Fatal("NewRegistry accepted duplicate tool name")
	}
	if _, err := NewRegistry(&stubTool{name: "  "}); err == nil {
		t.Fatal("NewRegistry accepted blank tool name")
	}
	if _, err := NewRegistry(nil); err == nil {
		t.Fatal("NewRegistry accepted nil tool")
	}
}

func TestRegistrySpecsAndNamesInOrder(t *testing.T) {
	registry, err := NewRegistry(
		&stubTool{name: "first", result: json.RawMessage("{}")},
		&stubTool{name: "second", result: json.RawMessage("{}")},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	names := registry.Names()
	if len(names) != 2 || names[0] != "first" || names[1] != "second" {
		t.Fatalf("Names() = %#v", names)
	}
	specs := registry.Specs()
	if len(specs) != 2 || specs[0].Function.Name != "first" || specs[0].Type != "function" {
		t.Fatalf("Specs() = %#v", specs)
	}
}

func TestRegistryInvokeUnknownToolFailsFast(t *testing.T) {
	registry, err := NewRegistry(&stubTool{name: "known", result: json.RawMessage("{}")})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if _, err := registry.Invoke(context.Background(), "ghost", nil); err == nil {
		t.Fatal("Invoke accepted unknown tool")
	}
}

func TestRegistryInvokePropagatesToolError(t *testing.T) {
	boom := errors.New("boom")
	registry, err := NewRegistry(&stubTool{name: "known", err: boom})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if _, err := registry.Invoke(context.Background(), "known", json.RawMessage("{}")); !errors.Is(err, boom) {
		t.Fatalf("Invoke() error = %v, want wrapped boom", err)
	}
}

func TestRegistryInvokeRejectsEmptyResult(t *testing.T) {
	registry, err := NewRegistry(&stubTool{name: "known", result: json.RawMessage("")})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if _, err := registry.Invoke(context.Background(), "known", json.RawMessage("{}")); err == nil {
		t.Fatal("Invoke accepted empty tool result")
	}
}
