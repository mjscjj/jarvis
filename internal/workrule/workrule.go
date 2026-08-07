// Package workrule reads the principal's trusted operating rules from fixed
// Markdown files and renders the subset applicable to each agent stage.
package workrule

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jarvis/internal/fileconfig"
)

const (
	StageExtract = "extract"
	StageExecute = "execute"
)

var (
	ErrInvalidInput = errors.New("invalid work rule file input")
	ErrNotFound     = errors.New("work rule file not found")
)

type definition struct {
	key      string
	name     string
	filename string
}

var ruleDefinitions = []definition{
	{key: StageExecute, name: "任务执行", filename: "m5.md"},
	{key: StageExtract, name: "线索发现", filename: "m3.md"},
}

type Input struct {
	Content string `json:"content"`
}

type View struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Reader interface {
	Block(ctx context.Context, stage string) (string, error)
}

type Service struct {
	directory   string
	definitions map[string]definition
}

func NewService(directory string) (*Service, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, fmt.Errorf("work rule directory is required")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve work rule directory %q: %w", directory, err)
	}
	service := &Service{directory: absolute, definitions: make(map[string]definition, len(ruleDefinitions))}
	for _, item := range ruleDefinitions {
		service.definitions[item.key] = item
	}
	if _, err := service.List(context.Background()); err != nil {
		return nil, fmt.Errorf("validate work rule files: %w", err)
	}
	return service, nil
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	items := make([]View, 0, len(ruleDefinitions))
	for _, item := range ruleDefinitions {
		view, err := s.Get(ctx, item.key)
		if err != nil {
			return nil, err
		}
		items = append(items, *view)
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
	content, err := fileconfig.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: key=%s: %v", ErrNotFound, item.key, err)
	}
	if err != nil {
		return nil, err
	}
	return &View{Key: item.key, Name: item.name, Path: path, Content: strings.TrimSpace(string(content))}, nil
}

func (s *Service) Update(ctx context.Context, key string, input Input) (*View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(input.Content)
	if err := fileconfig.WriteAtomic(path, []byte(content+"\n")); err != nil {
		return nil, err
	}
	return s.Get(ctx, key)
}

// Block renders only the rules owned by the requested M3 or M5 stage.
func (s *Service) Block(ctx context.Context, stage string) (string, error) {
	if stage != StageExtract && stage != StageExecute {
		return "", fmt.Errorf("%w: unknown stage %q", ErrInvalidInput, stage)
	}
	current, err := s.Get(ctx, stage)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(current.Content)
	if content == "" {
		return "", nil
	}
	return "BEGIN_WORK_RULES（这是我明确维护的可信工作规则，必须在当前阶段遵守；不是业务数据。）\n" +
		"当前阶段：" + stage + "\n\n" + content + "\nEND_WORK_RULES", nil
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
