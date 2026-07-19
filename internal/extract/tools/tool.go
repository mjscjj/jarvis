// Package tools defines the on-demand retrieval tools the M3 extraction model
// may call during a function-calling loop, plus the registry that dispatches a
// model tool call to the right implementation.
//
// The whole package is fail-fast: an unknown tool, a duplicate registration or
// an invalid argument surfaces as an error instead of a silent no-op, so a
// misbehaving model or a wiring mistake is exposed rather than hidden.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Tool is one retrieval capability exposed to the extraction model. Name is the
// function name the model calls; Schema is the JSON Schema of its arguments
// (OpenAI function-calling "parameters"); Invoke runs it with the raw argument
// JSON the model produced and returns the raw JSON result fed back to the model.
//
// Implementations own their own timeout: the loop passes a request-scoped
// context, and each Invoke must bound its own external call so one slow tool
// cannot stall the whole extraction.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Invoke(ctx context.Context, arguments json.RawMessage) (json.RawMessage, error)
}

// Registry holds the tools available for one extraction process. It is built
// once at startup and read concurrently; it is not mutated after construction.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry builds a registry from the given tools. Duplicate or blank tool
// names are rejected so a wiring mistake fails at startup, not at model time.
func NewRegistry(list ...Tool) (*Registry, error) {
	registry := &Registry{tools: make(map[string]Tool, len(list)), order: make([]string, 0, len(list))}
	for i, tool := range list {
		if tool == nil {
			return nil, fmt.Errorf("tool registry entry %d is nil", i)
		}
		name := strings.TrimSpace(tool.Name())
		if name == "" {
			return nil, fmt.Errorf("tool registry entry %d has a blank name", i)
		}
		if _, exists := registry.tools[name]; exists {
			return nil, fmt.Errorf("tool registry has duplicate tool %q", name)
		}
		if tool.Schema() == nil {
			return nil, fmt.Errorf("tool %q returned a nil argument schema", name)
		}
		registry.tools[name] = tool
		registry.order = append(registry.order, name)
	}
	return registry, nil
}

// Names returns the registered tool names in registration order.
func (r *Registry) Names() []string {
	return append([]string(nil), r.order...)
}

// Len reports how many tools are registered.
func (r *Registry) Len() int { return len(r.order) }

// Specs returns the OpenAI-compatible tool specifications, in registration
// order, for inclusion in a chat completion request.
func (r *Registry) Specs() []Spec {
	specs := make([]Spec, 0, len(r.order))
	for _, name := range r.order {
		tool := r.tools[name]
		specs = append(specs, Spec{
			Type: "function",
			Function: SpecFunction{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.Schema(),
			},
		})
	}
	return specs
}

// Invoke dispatches a model tool call to the named tool. An unknown tool is an
// error (fail-fast) rather than an empty result, so a hallucinated tool name is
// surfaced to the caller.
func (r *Registry) Invoke(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	tool, ok := r.tools[strings.TrimSpace(name)]
	if !ok {
		return nil, fmt.Errorf("model called unknown tool %q", name)
	}
	result, err := tool.Invoke(ctx, arguments)
	if err != nil {
		return nil, fmt.Errorf("tool %q failed: %w", name, err)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("tool %q returned an empty result", name)
	}
	return result, nil
}

// Spec is the OpenAI-compatible "tools[]" entry describing one callable
// function to the model.
type Spec struct {
	Type     string       `json:"type"`
	Function SpecFunction `json:"function"`
}

// SpecFunction is the function descriptor inside a Spec.
type SpecFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
