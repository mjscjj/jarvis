package skill

import (
	"context"
	"fmt"
)

type Renderer func(string) string

// RenderingService decorates the repository-backed Skill service for runtime
// consumption. The source files remain unchanged and stay the only truth.
type RenderingService struct {
	source *Service
	render Renderer
}

func NewRenderingService(source *Service, render Renderer) (*RenderingService, error) {
	if source == nil {
		return nil, fmt.Errorf("rendered skill source is nil")
	}
	if render == nil {
		return nil, fmt.Errorf("rendered skill renderer is nil")
	}
	return &RenderingService{source: source, render: render}, nil
}

func (s *RenderingService) List(ctx context.Context) ([]View, error) {
	items, err := s.source.List(ctx)
	if err != nil {
		return nil, err
	}
	return s.renderViews(items), nil
}

func (s *RenderingService) Scan(ctx context.Context) ([]View, error) {
	items, err := s.source.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return s.renderViews(items), nil
}

func (s *RenderingService) Update(ctx context.Context, name string, input Input) (*View, error) {
	item, err := s.source.Update(ctx, name, input)
	if err != nil {
		return nil, err
	}
	rendered := *item
	rendered.Description = s.render(rendered.Description)
	return &rendered, nil
}

func (s *RenderingService) Content(ctx context.Context, name string) (*ContentView, error) {
	item, err := s.source.Content(ctx, name)
	if err != nil {
		return nil, err
	}
	rendered := *item
	rendered.Content = s.render(rendered.Content)
	return &rendered, nil
}

func (s *RenderingService) Catalog(ctx context.Context, stage string) (string, error) {
	content, err := s.source.Catalog(ctx, stage)
	if err != nil {
		return "", err
	}
	return s.render(content), nil
}

func (s *RenderingService) renderViews(items []View) []View {
	rendered := make([]View, len(items))
	copy(rendered, items)
	for i := range rendered {
		rendered[i].Description = s.render(rendered[i].Description)
	}
	return rendered
}
