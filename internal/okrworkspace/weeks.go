package okrworkspace

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WeekView struct {
	Quarter  string    `json:"quarter"`
	Week     string    `json:"week"`
	OpenedBy string    `json:"opened_by"`
	OpenedAt time.Time `json:"opened_at"`
}

type WeekList struct {
	Quarter string     `json:"quarter"`
	Weeks   []WeekView `json:"weeks"`
}

type OpenWeekInput struct {
	Quarter  string `json:"quarter"`
	Week     string `json:"week"`
	OpenedBy string `json:"opened_by"`
}

type OpenWeekResult struct {
	Week    WeekView `json:"week"`
	Created bool     `json:"created"`
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
	var objectiveCount int64
	if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Where("quarter = ?", input.Quarter).Count(&objectiveCount).Error; err != nil {
		return OpenWeekResult{}, fmt.Errorf("check weekly report quarter: %w", err)
	}
	if objectiveCount == 0 {
		return OpenWeekResult{}, fmt.Errorf("quarter %s has no OKR objectives", input.Quarter)
	}
	row := domain.WeeklyReportWeek{Quarter: input.Quarter, Week: input.Week, OpenedBy: input.OpenedBy, OpenedAt: time.Now().UTC()}
	created := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if created.Error != nil {
		return OpenWeekResult{}, fmt.Errorf("open weekly report week: %w", created.Error)
	}
	if created.RowsAffected == 0 {
		if err := s.db.WithContext(ctx).First(&row, "quarter = ? AND week = ?", input.Quarter, input.Week).Error; err != nil {
			return OpenWeekResult{}, fmt.Errorf("read opened weekly report week: %w", err)
		}
	}
	return OpenWeekResult{Week: weekView(row), Created: created.RowsAffected == 1}, nil
}

func (s *Service) requireOpenWeek(ctx context.Context, quarter, week string) error {
	if !quarterPattern.MatchString(quarter) {
		return fmt.Errorf("quarter must use YYYY-Qn")
	}
	if !weekPattern.MatchString(week) {
		return fmt.Errorf("week must use YYYY-Www")
	}
	var row domain.WeeklyReportWeek
	if err := s.db.WithContext(ctx).First(&row, "quarter = ? AND week = ?", quarter, week).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("weekly report week %s/%s is not open", quarter, week)
		}
		return fmt.Errorf("check weekly report week: %w", err)
	}
	return nil
}

func weekView(row domain.WeeklyReportWeek) WeekView {
	return WeekView{Quarter: row.Quarter, Week: row.Week, OpenedBy: row.OpenedBy, OpenedAt: row.OpenedAt.UTC()}
}
