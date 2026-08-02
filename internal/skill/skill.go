// Package skill scans repository SKILL.md files and reads their Jarvis runtime
// enablement from a local YAML file.
package skill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"jarvis/internal/fileconfig"

	"gopkg.in/yaml.v3"
)

const (
	StageExtract = "extract"
	StageExecute = "execute"
)

var (
	ErrInvalidInput = errors.New("invalid agent skill input")
	ErrNotFound     = errors.New("agent skill not found")
	stageOrder      = map[string]int{StageExtract: 0, StageExecute: 1}
	skillName       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type Input struct {
	Stages    []string `json:"stages"`
	IsEnabled *bool    `json:"is_enabled"`
}

type View struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	FilePath    string   `json:"file_path"`
	Stages      []string `json:"stages"`
	IsEnabled   bool     `json:"is_enabled"`
}

type ContentView struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Reader interface {
	Catalog(ctx context.Context, stage string) (string, error)
}

type Service struct {
	root       string
	configPath string
	mu         sync.Mutex
}

type configFile struct {
	Skills []setting `yaml:"skills"`
}

type setting struct {
	Name      string   `yaml:"name"`
	IsEnabled bool     `yaml:"enabled"`
	Stages    []string `yaml:"stages"`
}

type metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	FilePath    string
}

func NewService(root, configPath string) (*Service, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("agent skill root is empty")
	}
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil, fmt.Errorf("agent skill config path is empty")
	}
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve agent skill root %q: %w", root, err)
	}
	configAbsolute, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve agent skill config %q: %w", configPath, err)
	}
	service := &Service{root: rootAbsolute, configPath: configAbsolute}
	if _, err := service.List(context.Background()); err != nil {
		return nil, fmt.Errorf("validate agent skill files: %w", err)
	}
	return service, nil
}

// Scan re-reads the filesystem and validates the YAML mapping. It does not
// create database cache rows or silently add missing configuration.
func (s *Service) Scan(ctx context.Context) ([]View, error) {
	return s.List(ctx)
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	metadataByName, err := s.scanMetadata()
	if err != nil {
		return nil, err
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	return join(metadataByName, cfg)
}

func (s *Service) Update(ctx context.Context, name string, input Input) (*View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if !skillName.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid skill name %q", ErrInvalidInput, name)
	}
	stages, err := normalizeStages(input.Stages)
	if err != nil {
		return nil, err
	}
	if input.IsEnabled == nil {
		return nil, fmt.Errorf("%w: is_enabled is required", ErrInvalidInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	metadataByName, err := s.scanMetadata()
	if err != nil {
		return nil, err
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	if _, err := join(metadataByName, cfg); err != nil {
		return nil, err
	}
	found := false
	for i := range cfg.Skills {
		if cfg.Skills[i].Name != name {
			continue
		}
		cfg.Skills[i].Stages = stages
		cfg.Skills[i].IsEnabled = *input.IsEnabled
		found = true
		break
	}
	if !found {
		return nil, fmt.Errorf("%w: name=%s", ErrNotFound, name)
	}
	sort.Slice(cfg.Skills, func(i, j int) bool { return cfg.Skills[i].Name < cfg.Skills[j].Name })
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode agent skill config: %w", err)
	}
	if err := fileconfig.WriteAtomic(s.configPath, encoded); err != nil {
		return nil, err
	}
	views, err := join(metadataByName, cfg)
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].Name == name {
			return &views[i], nil
		}
	}
	return nil, fmt.Errorf("%w: name=%s", ErrNotFound, name)
}

func (s *Service) Content(ctx context.Context, name string) (*ContentView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if !skillName.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid skill name %q", ErrInvalidInput, name)
	}
	items, err := s.scanMetadata()
	if err != nil {
		return nil, err
	}
	item, ok := items[name]
	if !ok {
		return nil, fmt.Errorf("%w: name=%s", ErrNotFound, name)
	}
	path := filepath.Join(s.root, filepath.FromSlash(item.FilePath))
	raw, err := fileconfig.Read(path)
	if err != nil {
		return nil, err
	}
	return &ContentView{Name: name, Path: path, Content: string(raw)}, nil
}

func (s *Service) Catalog(ctx context.Context, stage string) (string, error) {
	if _, ok := stageOrder[stage]; !ok {
		return "", fmt.Errorf("%w: unknown stage %q", ErrInvalidInput, stage)
	}
	views, err := s.List(ctx)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0)
	for _, item := range views {
		if !item.IsEnabled || !contains(item.Stages, stage) {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s：%s\n  读取：jarvis-tools get-skill --name %s", item.Name, item.Description, item.Name))
	}
	if len(lines) == 0 {
		return "", nil
	}
	return "BEGIN_AVAILABLE_SKILLS\n当前阶段：" + stage + "\n任务匹配可用 Skill 时，先读取对应 Skill，再按其说明执行。\n" + strings.Join(lines, "\n") + "\nEND_AVAILABLE_SKILLS", nil
}

func (s *Service) scanMetadata() (map[string]metadata, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("scan agent skill root %q: %w", s.root, err)
	}
	items := make(map[string]metadata)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(s.root, entry.Name(), "SKILL.md")
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read agent skill %q: %w", path, err)
		}
		item, err := parseMetadata(raw)
		if err != nil {
			return nil, fmt.Errorf("parse agent skill %q: %w", path, err)
		}
		if _, exists := items[item.Name]; exists {
			return nil, fmt.Errorf("duplicate agent skill name %q", item.Name)
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil, fmt.Errorf("relativize agent skill %q: %w", path, err)
		}
		item.FilePath = filepath.ToSlash(relative)
		items[item.Name] = item
	}
	return items, nil
}

func (s *Service) loadConfig() (configFile, error) {
	raw, err := fileconfig.Read(s.configPath)
	if err != nil {
		return configFile{}, err
	}
	var cfg configFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return configFile{}, fmt.Errorf("decode agent skill config %s: %w", s.configPath, err)
	}
	seen := make(map[string]struct{}, len(cfg.Skills))
	for i := range cfg.Skills {
		item := &cfg.Skills[i]
		item.Name = strings.TrimSpace(item.Name)
		if !skillName.MatchString(item.Name) {
			return configFile{}, fmt.Errorf("%w: invalid configured skill name %q", ErrInvalidInput, item.Name)
		}
		if _, exists := seen[item.Name]; exists {
			return configFile{}, fmt.Errorf("%w: duplicate configured skill %q", ErrInvalidInput, item.Name)
		}
		seen[item.Name] = struct{}{}
		stages, err := normalizeStages(item.Stages)
		if err != nil {
			return configFile{}, fmt.Errorf("skill %s: %w", item.Name, err)
		}
		item.Stages = stages
	}
	return cfg, nil
}

func join(metadataByName map[string]metadata, cfg configFile) ([]View, error) {
	settings := make(map[string]setting, len(cfg.Skills))
	for _, item := range cfg.Skills {
		settings[item.Name] = item
		if _, ok := metadataByName[item.Name]; !ok {
			return nil, fmt.Errorf("%w: configured skill %q has no SKILL.md", ErrInvalidInput, item.Name)
		}
	}
	views := make([]View, 0, len(metadataByName))
	for name, item := range metadataByName {
		control, ok := settings[name]
		if !ok {
			return nil, fmt.Errorf("%w: skill %q is missing from skills.yaml", ErrInvalidInput, name)
		}
		views = append(views, View{
			Name: name, Description: item.Description, FilePath: item.FilePath,
			Stages: append([]string(nil), control.Stages...), IsEnabled: control.IsEnabled,
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })
	return views, nil
}

func parseMetadata(raw []byte) (metadata, error) {
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return metadata{}, fmt.Errorf("SKILL.md must start with YAML frontmatter")
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return metadata{}, fmt.Errorf("SKILL.md frontmatter is not closed")
	}
	var item metadata
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &item); err != nil {
		return metadata{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	item.Name = strings.TrimSpace(item.Name)
	item.Description = strings.TrimSpace(item.Description)
	if !skillName.MatchString(item.Name) || item.Description == "" {
		return metadata{}, fmt.Errorf("frontmatter requires a valid name and non-empty description")
	}
	return item, nil
}

func normalizeStages(input []string) ([]string, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("%w: at least one stage is required", ErrInvalidInput)
	}
	seen := map[string]struct{}{}
	for _, stage := range input {
		if _, ok := stageOrder[stage]; !ok {
			return nil, fmt.Errorf("%w: unknown stage %q", ErrInvalidInput, stage)
		}
		seen[stage] = struct{}{}
	}
	stages := make([]string, 0, len(seen))
	for stage := range seen {
		stages = append(stages, stage)
	}
	sort.Slice(stages, func(i, j int) bool { return stageOrder[stages[i]] < stageOrder[stages[j]] })
	return stages, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
