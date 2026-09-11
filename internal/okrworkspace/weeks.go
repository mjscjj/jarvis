package okrworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WeekView struct {
	Quarter     string                 `json:"quarter"`
	Week        string                 `json:"week"`
	TemplateKey domain.WeekTemplateKey `json:"template_key"`
	OpenedBy    string                 `json:"opened_by"`
	OpenedAt    time.Time              `json:"opened_at"`
}

type WeekList struct {
	Quarter string     `json:"quarter"`
	Weeks   []WeekView `json:"weeks"`
}

type OpenWeekInput struct {
	Quarter     string                 `json:"quarter"`
	Week        string                 `json:"week"`
	TemplateKey domain.WeekTemplateKey `json:"template_key"`
	OpenedBy    string                 `json:"opened_by"`
}

type OpenWeekResult struct {
	Week    WeekView `json:"week"`
	Created bool     `json:"created"`
}

var (
	ErrWeekNotFound         = errors.New("weekly report week not found")
	ErrWeekTemplateConflict = errors.New("weekly report week template conflict")
)

type DeleteWeekResult struct {
	Quarter  string `json:"quarter"`
	Week     string `json:"week"`
	NextWeek string `json:"next_week,omitempty"`
}

func (s *Service) ListWeeks(ctx context.Context, quarter string) (WeekList, error) {
	quarter = strings.TrimSpace(quarter)
	if !quarterPattern.MatchString(quarter) {
		return WeekList{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	var rows []domain.WeeklyReportWeek
	if err := s.db.WithContext(ctx).Where("quarter = ?", quarter).Order("week DESC").Find(&rows).Error; err != nil {
		return WeekList{}, fmt.Errorf("list weekly report weeks: %w", err)
	}
	result := WeekList{Quarter: quarter, Weeks: make([]WeekView, 0, len(rows))}
	for _, row := range rows {
		result.Weeks = append(result.Weeks, weekView(row))
	}
	return result, nil
}

func (s *Service) OpenWeek(ctx context.Context, input OpenWeekInput) (OpenWeekResult, error) {
	input.Quarter = strings.TrimSpace(input.Quarter)
	input.Week = strings.TrimSpace(input.Week)
	input.OpenedBy = strings.TrimSpace(input.OpenedBy)
	if !quarterPattern.MatchString(input.Quarter) {
		return OpenWeekResult{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	if !weekPattern.MatchString(input.Week) {
		return OpenWeekResult{}, fmt.Errorf("week must use YYYY-Www")
	}
	if !domain.ValidWeekTemplateKey(input.TemplateKey) {
		return OpenWeekResult{}, fmt.Errorf("template_key must be %q or %q", domain.WeekTemplateClassic, domain.WeekTemplateOKRPreview)
	}
	var objectiveCount int64
	if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Where("quarter = ? AND plan_id = ''", input.Quarter).Count(&objectiveCount).Error; err != nil {
		return OpenWeekResult{}, fmt.Errorf("check weekly report quarter: %w", err)
	}
	if objectiveCount == 0 {
		return OpenWeekResult{}, fmt.Errorf("quarter %s has no OKR objectives", input.Quarter)
	}
	row := domain.WeeklyReportWeek{Quarter: input.Quarter, Week: input.Week, TemplateKey: input.TemplateKey, OpenedBy: input.OpenedBy, OpenedAt: time.Now().UTC()}
	created := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if created.Error != nil {
		return OpenWeekResult{}, fmt.Errorf("open weekly report week: %w", created.Error)
	}
	if created.RowsAffected == 0 {
		if err := s.db.WithContext(ctx).Unscoped().First(&row, "quarter = ? AND week = ?", input.Quarter, input.Week).Error; err != nil {
			return OpenWeekResult{}, fmt.Errorf("read opened weekly report week: %w", err)
		}
		if row.DeletedAt.Valid {
			return OpenWeekResult{}, fmt.Errorf("该周次已删除，不允许重新新建")
		}
		if row.TemplateKey != input.TemplateKey {
			return OpenWeekResult{}, fmt.Errorf("%w: %s/%s is %q, requested %q", ErrWeekTemplateConflict, input.Quarter, input.Week, row.TemplateKey, input.TemplateKey)
		}
	}
	return OpenWeekResult{Week: weekView(row), Created: created.RowsAffected == 1}, nil
}

// DeleteWeek soft-deletes the week anchor and preserves all associated data.
// The (quarter, week) key remains reserved and cannot be reopened.
func (s *Service) DeleteWeek(ctx context.Context, quarter, week, expectedToken string) (DeleteWeekResult, error) {
	quarter = strings.TrimSpace(quarter)
	week = strings.TrimSpace(week)
	expectedToken = strings.TrimSpace(expectedToken)
	if !quarterPattern.MatchString(quarter) {
		return DeleteWeekResult{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	if !weekPattern.MatchString(week) {
		return DeleteWeekResult{}, fmt.Errorf("week must use YYYY-Www")
	}
	if expectedToken == "" {
		return DeleteWeekResult{}, fmt.Errorf("delete_token is required")
	}
	if err := s.db.WithContext(ctx).First(&domain.WeeklyReportWeek{}, "quarter = ? AND week = ?", quarter, week).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DeleteWeekResult{}, ErrWeekNotFound
		}
		return DeleteWeekResult{}, fmt.Errorf("read weekly report week before delete: %w", err)
	}
	currentToken, err := s.weekDeletionToken(ctx, quarter, week)
	if err != nil {
		return DeleteWeekResult{}, fmt.Errorf("read weekly report deletion guard: %w", err)
	}
	if currentToken != expectedToken {
		return DeleteWeekResult{}, ErrConflict
	}

	result := DeleteWeekResult{Quarter: quarter, Week: week}
	anchor := s.db.WithContext(ctx).Where("quarter = ? AND week = ?", quarter, week).Delete(&domain.WeeklyReportWeek{})
	if anchor.Error != nil {
		return DeleteWeekResult{}, fmt.Errorf("delete weekly report week: %w", anchor.Error)
	}
	if anchor.RowsAffected != 1 {
		return DeleteWeekResult{}, ErrWeekNotFound
	}
	weeks, err := s.ListWeeks(ctx, quarter)
	if err != nil {
		return DeleteWeekResult{}, err
	}
	if len(weeks.Weeks) > 0 {
		result.NextWeek = weeks.Weeks[0].Week
	}
	return result, nil
}

func (s *Service) requireOpenWeek(ctx context.Context, quarter, week string) error {
	_, err := s.openedWeek(ctx, quarter, week)
	return err
}

func (s *Service) openedWeek(ctx context.Context, quarter, week string) (domain.WeeklyReportWeek, error) {
	if !quarterPattern.MatchString(quarter) {
		return domain.WeeklyReportWeek{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	if !weekPattern.MatchString(week) {
		return domain.WeeklyReportWeek{}, fmt.Errorf("week must use YYYY-Www")
	}
	var row domain.WeeklyReportWeek
	if err := s.db.WithContext(ctx).First(&row, "quarter = ? AND week = ?", quarter, week).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return domain.WeeklyReportWeek{}, fmt.Errorf("weekly report week %s/%s is not open", quarter, week)
		}
		return domain.WeeklyReportWeek{}, fmt.Errorf("check weekly report week: %w", err)
	}
	if !domain.ValidWeekTemplateKey(row.TemplateKey) {
		return domain.WeeklyReportWeek{}, fmt.Errorf("weekly report week %s/%s has invalid template_key %q", quarter, week, row.TemplateKey)
	}
	return row, nil
}

func weekView(row domain.WeeklyReportWeek) WeekView {
	return WeekView{Quarter: row.Quarter, Week: row.Week, TemplateKey: row.TemplateKey, OpenedBy: row.OpenedBy, OpenedAt: row.OpenedAt.UTC()}
}
