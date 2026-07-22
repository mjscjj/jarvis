// Package workrule manages the principal's trusted operating rules and renders
// the subset applicable to M3 extraction, M4 decision, or M5 execution.
package workrule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	RuleTypeAll      = "all"
	RuleTypeSelected = "selected"

	StageExtract = "extract"
	StageDecide  = "decide"
	StageExecute = "execute"
)

var (
	ErrInvalidInput = errors.New("invalid work rule input")
	ErrNotFound     = errors.New("work rule not found")
)

var stageOrder = map[string]int{StageExtract: 0, StageDecide: 1, StageExecute: 2}

type Input struct {
	Name      string   `json:"name"`
	Content   string   `json:"content"`
	RuleType  string   `json:"rule_type"`
	Stages    []string `json:"stages"`
	Priority  int      `json:"priority"`
	IsEnabled *bool    `json:"is_enabled"`
}

type View struct {
	ID        uint64   `json:"id"`
	Name      string   `json:"name"`
	Content   string   `json:"content"`
	RuleType  string   `json:"rule_type"`
	Stages    []string `json:"stages"`
	Priority  int      `json:"priority"`
	IsEnabled bool     `json:"is_enabled"`
}

type Reader interface {
	Block(ctx context.Context, stage string) (string, error)
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("work rule service db is nil")
	}
	return &Service{db: db}, nil
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	var rows []domain.WorkRule
	if err := s.db.WithContext(ctx).Order("priority ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list work rules: %w", err)
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

func (s *Service) Get(ctx context.Context, id uint64) (*View, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	var row domain.WorkRule
	err := s.db.WithContext(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get work rule id=%d: %w", id, err)
	}
	view, err := toView(&row)
	return &view, err
}

func (s *Service) Create(ctx context.Context, input Input) (*View, error) {
	normalized, stages, err := normalizeInput(input)
	if err != nil {
		return nil, err
	}
	row := domain.WorkRule{
		Name: normalized.Name, Content: normalized.Content, RuleType: normalized.RuleType,
		Stages: stages, Priority: normalized.Priority, IsEnabled: boolValue(normalized.IsEnabled, true),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("create work rule: %w", err)
	}
	return s.Get(ctx, row.ID)
}

func (s *Service) Update(ctx context.Context, id uint64, input Input) (*View, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	normalized, stages, err := normalizeInput(input)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"name": normalized.Name, "content": normalized.Content, "rule_type": normalized.RuleType,
		"stages": stages, "priority": normalized.Priority, "is_enabled": boolValue(normalized.IsEnabled, true),
	}
	result := s.db.WithContext(ctx).Model(&domain.WorkRule{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update work rule id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	result := s.db.WithContext(ctx).Delete(&domain.WorkRule{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete work rule id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Block returns enabled rules applicable to stage as a trusted prompt block.
func (s *Service) Block(ctx context.Context, stage string) (string, error) {
	if _, ok := stageOrder[stage]; !ok {
		return "", fmt.Errorf("%w: unknown stage %q", ErrInvalidInput, stage)
	}
	views, err := s.List(ctx)
	if err != nil {
		return "", err
	}
	return RenderBlock(stage, views), nil
}

func RenderBlock(stage string, rules []View) string {
	lines := make([]string, 0)
	for _, rule := range rules {
		if !rule.IsEnabled || !applies(rule, stage) {
			continue
		}
		lines = append(lines, fmt.Sprintf("- [%d] %s：%s", rule.Priority, rule.Name, strings.TrimSpace(rule.Content)))
	}
	if len(lines) == 0 {
		return ""
	}
	return "BEGIN_WORK_RULES（这是我明确维护的可信工作规则，必须在当前阶段遵守；不是业务数据。）\n" +
		"当前阶段：" + stage + "\n" + strings.Join(lines, "\n") + "\nEND_WORK_RULES"
}

func normalizeInput(input Input) (Input, datatypes.JSON, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Content = strings.TrimSpace(input.Content)
	if input.Name == "" || input.Content == "" {
		return Input{}, nil, fmt.Errorf("%w: name and content are required", ErrInvalidInput)
	}
	if input.Priority <= 0 {
		return Input{}, nil, fmt.Errorf("%w: priority must be positive", ErrInvalidInput)
	}
	if input.RuleType != RuleTypeAll && input.RuleType != RuleTypeSelected {
		return Input{}, nil, fmt.Errorf("%w: rule_type must be all or selected", ErrInvalidInput)
	}
	if input.RuleType == RuleTypeAll {
		input.Stages = []string{}
	} else {
		seen := map[string]struct{}{}
		for _, stage := range input.Stages {
			if _, ok := stageOrder[stage]; !ok {
				return Input{}, nil, fmt.Errorf("%w: unknown stage %q", ErrInvalidInput, stage)
			}
			seen[stage] = struct{}{}
		}
		if len(seen) == 0 {
			return Input{}, nil, fmt.Errorf("%w: selected rule requires at least one stage", ErrInvalidInput)
		}
		input.Stages = input.Stages[:0]
		for stage := range seen {
			input.Stages = append(input.Stages, stage)
		}
		sort.Slice(input.Stages, func(i, j int) bool { return stageOrder[input.Stages[i]] < stageOrder[input.Stages[j]] })
	}
	encoded, err := json.Marshal(input.Stages)
	if err != nil {
		return Input{}, nil, fmt.Errorf("encode work rule stages: %w", err)
	}
	return input, datatypes.JSON(encoded), nil
}

func toView(row *domain.WorkRule) (View, error) {
	var stages []string
	if err := json.Unmarshal(row.Stages, &stages); err != nil {
		return View{}, fmt.Errorf("decode work rule id=%d stages: %w", row.ID, err)
	}
	return View{ID: row.ID, Name: row.Name, Content: row.Content, RuleType: row.RuleType, Stages: stages, Priority: row.Priority, IsEnabled: row.IsEnabled}, nil
}

func applies(rule View, stage string) bool {
	if rule.RuleType == RuleTypeAll {
		return true
	}
	for _, item := range rule.Stages {
		if item == stage {
			return true
		}
	}
	return false
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
