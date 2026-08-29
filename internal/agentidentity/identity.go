// Package agentidentity owns the user-selected display identity of the local
// assistant. It has no dependency on business events, world state, or agent
// decision stages.
package agentidentity

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	Placeholder  = "{{AGENT_NAME}}"
	MaxNameRunes = 32
)

// ValidateName keeps the display name safe for single-line UI and prompt use.
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("agent display name is empty")
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return fmt.Errorf("agent display name exceeds %d characters", MaxNameRunes)
	}
	if strings.Contains(name, "{{") || strings.Contains(name, "}}") {
		return fmt.Errorf("agent display name contains reserved template syntax")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("agent display name contains a control character")
		}
	}
	return nil
}

type Renderer struct {
	name string
}

func NewRenderer(name string) (*Renderer, error) {
	name = strings.TrimSpace(name)
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	return &Renderer{name: name}, nil
}

func (r *Renderer) Name() string {
	return r.name
}

func (r *Renderer) Render(template string) string {
	return strings.ReplaceAll(template, Placeholder, r.name)
}

type ContentReader interface {
	Content(context.Context, string) (string, error)
}

type RenderingContentReader struct {
	source   ContentReader
	renderer *Renderer
}

func NewContentReader(source ContentReader, renderer *Renderer) (*RenderingContentReader, error) {
	if source == nil {
		return nil, fmt.Errorf("agent identity content source is nil")
	}
	if renderer == nil {
		return nil, fmt.Errorf("agent identity renderer is nil")
	}
	return &RenderingContentReader{source: source, renderer: renderer}, nil
}

func (r *RenderingContentReader) Content(ctx context.Context, key string) (string, error) {
	content, err := r.source.Content(ctx, key)
	if err != nil {
		return "", err
	}
	return r.renderer.Render(content), nil
}

type BlockReader interface {
	Block(context.Context, string) (string, error)
}

type RenderingBlockReader struct {
	source   BlockReader
	renderer *Renderer
}

func NewBlockReader(source BlockReader, renderer *Renderer) (*RenderingBlockReader, error) {
	if source == nil {
		return nil, fmt.Errorf("agent identity block source is nil")
	}
	if renderer == nil {
		return nil, fmt.Errorf("agent identity renderer is nil")
	}
	return &RenderingBlockReader{source: source, renderer: renderer}, nil
}

func (r *RenderingBlockReader) Block(ctx context.Context, stage string) (string, error) {
	content, err := r.source.Block(ctx, stage)
	if err != nil {
		return "", err
	}
	return r.renderer.Render(content), nil
}
