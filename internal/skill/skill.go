// Package skill scans repository SKILL.md files and exposes the enabled subset
// to M3 extraction, M4 decision, and M5 execution.
package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"jarvis/internal/domain"

	"gopkg.in/yaml.v3"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	StageExtract = "extract"
	StageDecide  = "decide"
	StageExecute = "execute"
)

var (
	ErrInvalidInput = errors.New("invalid agent skill input")
	ErrNotFound     = errors.New("agent skill not found")
	stageOrder      = map[string]int{StageExtract: 0, StageDecide: 1, StageExecute: 2}
	skillName       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type Input struct {
	Stages    []string `json:"stages"`
	IsEnabled *bool    `json:"is_enabled"`
}

type View struct {
	ID          uint64   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	FilePath    string   `json:"file_path"`
	Stages      []string `json:"stages"`
	IsEnabled   bool     `json:"is_enabled"`
}

type ContentView struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type Reader interface {
	Catalog(ctx context.Context, stage string) (string, error)
}

type Service struct {
	db   *gorm.DB
	root string
}

func NewService(db *gorm.DB, root string) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("agent skill service db is nil")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("agent skill root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve agent skill root %q: %w", root, err)
	}
	return &Service{db: db, root: abs}, nil
}

// Scan imports SKILL.md metadata while preserving the stage and enabled controls
// already set in the database. New skills are enabled for all three stages.
func (s *Service) Scan(ctx context.Context) ([]View, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("scan agent skill root %q: %w", s.root, err)
	}
	seen := make([]string, 0, len(entries))
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
		meta, err := parseMetadata(raw)
		if err != nil {
			return nil, fmt.Errorf("parse agent skill %q: %w", path, err)
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil, fmt.Errorf("relativize agent skill %q: %w", path, err)
		}
		seen = append(seen, meta.Name)

		var existing domain.AgentSkill
		result := s.db.WithContext(ctx).Where("name = ?", meta.Name).Limit(1).Find(&existing)
		if result.Error != nil {
			return nil, fmt.Errorf("find agent skill %q: %w", meta.Name, result.Error)
		}
		if result.RowsAffected == 0 {
			stages, err := json.Marshal([]string{StageExtract, StageDecide, StageExecute})
			if err != nil {
				return nil, fmt.Errorf("encode default stages: %w", err)
			}
			row := domain.AgentSkill{Name: meta.Name, Description: meta.Description, FilePath: filepath.ToSlash(rel), Stages: stages, IsEnabled: true}
			if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
				return nil, fmt.Errorf("create agent skill %q: %w", meta.Name, err)
			}
			continue
		}
		updates := map[string]any{"description": meta.Description, "file_path": filepath.ToSlash(rel)}
		if err := s.db.WithContext(ctx).Model(&domain.AgentSkill{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("refresh agent skill %q: %w", meta.Name, err)
		}
	}
	if len(seen) == 0 {
		if err := s.db.WithContext(ctx).Where("1 = 1").Delete(&domain.AgentSkill{}).Error; err != nil {
			return nil, fmt.Errorf("remove stale agent skills: %w", err)
		}
	} else if err := s.db.WithContext(ctx).Where("name NOT IN ?", seen).Delete(&domain.AgentSkill{}).Error; err != nil {
		return nil, fmt.Errorf("remove stale agent skills: %w", err)
	}
	return s.List(ctx)
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	var rows []domain.AgentSkill
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list agent skills: %w", err)
	}
	views := make([]View, len(rows))
	for i := range rows {
		view, err := toView(&rows[i])
		if err != nil {
			return nil, err
		}
		views[i] = view
	}
	return views, nil
}

func (s *Service) Update(ctx context.Context, id uint64, input Input) (*View, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	stages, encoded, err := normalizeStages(input.Stages)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{"stages": datatypes.JSON(encoded)}
	if input.IsEnabled != nil {
		updates["is_enabled"] = *input.IsEnabled
	}
	result := s.db.WithContext(ctx).Model(&domain.AgentSkill{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update agent skill id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	var row domain.AgentSkill
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, fmt.Errorf("reload agent skill id=%d: %w", id, err)
	}
	view, err := toView(&row)
	if err != nil {
		return nil, err
	}
	view.Stages = stages
	return &view, nil
}

func (s *Service) Content(ctx context.Context, name string) (*ContentView, error) {
	name = strings.TrimSpace(name)
	if !skillName.MatchString(name) {
		return nil, fmt.Errorf("%w: invalid skill name %q", ErrInvalidInput, name)
	}
	var row domain.AgentSkill
	err := s.db.WithContext(ctx).Where("name = ?", name).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get agent skill %q: %w", name, err)
	}
	path, err := s.resolvePath(row.FilePath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent skill %q: %w", name, err)
	}
	return &ContentView{Name: row.Name, Content: string(raw)}, nil
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

func (s *Service) resolvePath(rel string) (string, error) {
	path := filepath.Join(s.root, filepath.FromSlash(rel))
	clean, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve agent skill path %q: %w", rel, err)
	}
	inside, err := filepath.Rel(s.root, clean)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("agent skill path escapes root: %q", rel)
	}
	return clean, nil
}

type metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
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
	var meta metadata
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &meta); err != nil {
		return metadata{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Description = strings.TrimSpace(meta.Description)
	if !skillName.MatchString(meta.Name) || meta.Description == "" {
		return metadata{}, fmt.Errorf("frontmatter requires a valid name and non-empty description")
	}
	return meta, nil
}

func normalizeStages(input []string) ([]string, []byte, error) {
	if len(input) == 0 {
		return nil, nil, fmt.Errorf("%w: at least one stage is required", ErrInvalidInput)
	}
	seen := map[string]struct{}{}
	for _, stage := range input {
		if _, ok := stageOrder[stage]; !ok {
			return nil, nil, fmt.Errorf("%w: unknown stage %q", ErrInvalidInput, stage)
		}
		seen[stage] = struct{}{}
	}
	stages := make([]string, 0, len(seen))
	for stage := range seen {
		stages = append(stages, stage)
	}
	sort.Slice(stages, func(i, j int) bool { return stageOrder[stages[i]] < stageOrder[stages[j]] })
	encoded, err := json.Marshal(stages)
	if err != nil {
		return nil, nil, fmt.Errorf("encode agent skill stages: %w", err)
	}
	return stages, encoded, nil
}

func toView(row *domain.AgentSkill) (View, error) {
	stages, _, err := normalizeRawStages(row.ID, row.Stages)
	if err != nil {
		return View{}, err
	}
	return View{ID: row.ID, Name: row.Name, Description: row.Description, FilePath: row.FilePath, Stages: stages, IsEnabled: row.IsEnabled}, nil
}

func normalizeRawStages(id uint64, raw []byte) ([]string, []byte, error) {
	var stages []string
	if err := json.Unmarshal(raw, &stages); err != nil {
		return nil, nil, fmt.Errorf("decode agent skill id=%d stages: %w", id, err)
	}
	normalized, encoded, err := normalizeStages(stages)
	if err != nil {
		return nil, nil, fmt.Errorf("decode agent skill id=%d stages: %w", id, err)
	}
	return normalized, encoded, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
