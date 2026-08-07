// Package textstore manages the fixed set of human-editable Markdown files
// used as runtime prompts and policies.
package textstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jarvis/internal/prompttemplate"
)

var (
	ErrInvalidInput = errors.New("invalid text file input")
	ErrNotFound     = errors.New("text file not found")
)

type Input struct {
	Content string `json:"content"`
}

type View struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Stage       string `json:"stage"`
	Path        string `json:"path"`
	Content     string `json:"content"`
}

type Reader interface {
	Content(ctx context.Context, key string) (string, error)
}

type Service struct {
	directory   string
	definitions map[string]definition
	order       []string
}

func NewService(directory string) (*Service, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, fmt.Errorf("text file directory is required")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve text file directory %q: %w", directory, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("open text file directory %s: %w", absolute, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("text file path is not a directory: %s", absolute)
	}

	service := &Service{
		directory:   absolute,
		definitions: make(map[string]definition),
		order:       make([]string, 0, len(definitions())),
	}
	for _, item := range definitions() {
		if _, exists := service.definitions[item.key]; exists {
			return nil, fmt.Errorf("duplicate text file key %q", item.key)
		}
		service.definitions[item.key] = item
		service.order = append(service.order, item.key)
	}
	if _, err := service.List(context.Background()); err != nil {
		return nil, fmt.Errorf("validate text files: %w", err)
	}
	return service, nil
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	items := make([]View, 0, len(s.order))
	for _, key := range s.order {
		item, err := s.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, key string) (*View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	item, path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: key=%s path=%s", ErrNotFound, item.key, path)
	}
	if err != nil {
		return nil, fmt.Errorf("read text file key=%s path=%s: %w", item.key, path, err)
	}
	normalized := strings.TrimSpace(string(content))
	if normalized == "" {
		return nil, fmt.Errorf("%w: key=%s has empty content", ErrInvalidInput, item.key)
	}
	if err := validateContent(item, normalized); err != nil {
		return nil, err
	}
	return &View{
		Key: item.key, Name: item.name, Description: item.description,
		Kind: item.kind, Stage: item.stage,
		Path: path, Content: normalized,
	}, nil
}

func (s *Service) Update(ctx context.Context, key string, input Input) (*View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	item, path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return nil, fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	if err := validateContent(item, content); err != nil {
		return nil, err
	}

	temp, err := os.CreateTemp(s.directory, "."+item.filename+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary text file key=%s: %w", item.key, err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return nil, fmt.Errorf("chmod temporary text file key=%s: %w", item.key, err)
	}
	if _, err := temp.WriteString(content + "\n"); err != nil {
		_ = temp.Close()
		return nil, fmt.Errorf("write temporary text file key=%s: %w", item.key, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return nil, fmt.Errorf("sync temporary text file key=%s: %w", item.key, err)
	}
	if err := temp.Close(); err != nil {
		return nil, fmt.Errorf("close temporary text file key=%s: %w", item.key, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return nil, fmt.Errorf("replace text file key=%s path=%s: %w", item.key, path, err)
	}
	return s.Get(ctx, key)
}

func validateContent(item definition, content string) error {
	var stage string
	switch item.key {
	case SystemPromptM3Key:
		stage = prompttemplate.StageM3
	case SystemPromptM5Key:
		stage = prompttemplate.StageM5
	default:
		return nil
	}
	if err := prompttemplate.Validate(stage, content); err != nil {
		return fmt.Errorf("%w: key=%s: %v", ErrInvalidInput, item.key, err)
	}
	return nil
}

func (s *Service) Content(ctx context.Context, key string) (string, error) {
	item, err := s.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return item.Content, nil
}

func (s *Service) resolve(key string) (definition, string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return definition{}, "", fmt.Errorf("%w: key is required", ErrInvalidInput)
	}
	item, ok := s.definitions[key]
	if !ok {
		return definition{}, "", fmt.Errorf("%w: key=%s", ErrNotFound, key)
	}
	return item, filepath.Join(s.directory, item.filename), nil
}
