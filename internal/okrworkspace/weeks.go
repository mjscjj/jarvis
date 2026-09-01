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

type DeleteWeekCounts struct {
	WeeklyCores     int64 `json:"weekly_cores"`
	Progress        int64 `json:"progress"`
	Scores          int64 `json:"scores"`
	Comments        int64 `json:"comments"`
	MeegoSnapshots  int64 `json:"meego_snapshots"`
	ReminderBatches int64 `json:"reminder_batches"`
}

type DeleteWeekResult struct {
	Quarter  string           `json:"quarter"`
	Week     string           `json:"week"`
	NextWeek string           `json:"next_week,omitempty"`
	Deleted  DeleteWeekCounts `json:"deleted"`
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
	if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Where("quarter = ?", input.Quarter).Count(&objectiveCount).Error; err != nil {
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
		if err := s.db.WithContext(ctx).First(&row, "quarter = ? AND week = ?", input.Quarter, input.Week).Error; err != nil {
			return OpenWeekResult{}, fmt.Errorf("read opened weekly report week: %w", err)
		}
		if row.TemplateKey != input.TemplateKey {
			return OpenWeekResult{}, fmt.Errorf("%w: %s/%s is %q, requested %q", ErrWeekTemplateConflict, input.Quarter, input.Week, row.TemplateKey, input.TemplateKey)
		}
	}
	return OpenWeekResult{Week: weekView(row), Created: created.RowsAffected == 1}, nil
}

// DeleteWeek removes one complete weekly-report scope while preserving the
// stable OKR definition and every other week. The week anchor is deleted last
// so an interrupted deletion remains visible and can be retried safely.
func (s *Service) DeleteWeek(ctx context.Context, quarter, week string) (DeleteWeekResult, error) {
	quarter = strings.TrimSpace(quarter)
	week = strings.TrimSpace(week)
	if !quarterPattern.MatchString(quarter) {
		return DeleteWeekResult{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	if !weekPattern.MatchString(week) {
		return DeleteWeekResult{}, fmt.Errorf("week must use YYYY-Www")
	}
	if err := s.db.WithContext(ctx).First(&domain.WeeklyReportWeek{}, "quarter = ? AND week = ?", quarter, week).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DeleteWeekResult{}, ErrWeekNotFound
		}
		return DeleteWeekResult{}, fmt.Errorf("read weekly report week before delete: %w", err)
	}

	var krIDs []string
	if err := s.db.WithContext(ctx).Model(&domain.KR{}).
		Joins("JOIN okr_workspace_objective ON okr_workspace_objective.id = okr_workspace_kr.objective_id").
		Where("okr_workspace_objective.quarter = ?", quarter).
		Pluck("okr_workspace_kr.id", &krIDs).Error; err != nil {
		return DeleteWeekResult{}, fmt.Errorf("list weekly report KRs before delete: %w", err)
	}
	var pointIDs []string
	if len(krIDs) > 0 {
		if err := s.db.WithContext(ctx).Model(&domain.KRPoint{}).Where("kr_id IN ?", krIDs).Pluck("id", &pointIDs).Error; err != nil {
			return DeleteWeekResult{}, fmt.Errorf("list weekly report points before delete: %w", err)
		}
	}

	result := DeleteWeekResult{Quarter: quarter, Week: week}
	comments := s.db.WithContext(ctx).Where("quarter = ? AND week = ?", quarter, week).Delete(&domain.PageComment{})
	if comments.Error != nil {
		return DeleteWeekResult{}, fmt.Errorf("delete weekly report comments: %w", comments.Error)
	}
	result.Deleted.Comments = comments.RowsAffected

	if len(pointIDs) > 0 {
		progress := s.db.WithContext(ctx).Where("point_id IN ? AND week = ?", pointIDs, week).Delete(&domain.KRProgress{})
		if progress.Error != nil {
			return DeleteWeekResult{}, fmt.Errorf("delete weekly report progress: %w", progress.Error)
		}
		result.Deleted.Progress = progress.RowsAffected

		snapshots := s.db.WithContext(ctx).Where("point_id IN ? AND week = ?", pointIDs, week).Delete(&domain.MeegoSyncSnapshot{})
		if snapshots.Error != nil {
			return DeleteWeekResult{}, fmt.Errorf("delete weekly report Meego snapshots: %w", snapshots.Error)
		}
		result.Deleted.MeegoSnapshots = snapshots.RowsAffected
	}

	scores := s.db.WithContext(ctx).Where("quarter = ? AND week = ?", quarter, week).Delete(&domain.WeeklyScore{})
	if scores.Error != nil {
		return DeleteWeekResult{}, fmt.Errorf("delete weekly report scores: %w", scores.Error)
	}
	result.Deleted.Scores = scores.RowsAffected

	if len(krIDs) > 0 {
		cores := s.db.WithContext(ctx).Where("kr_id IN ? AND week = ?", krIDs, week).Delete(&domain.WeeklyKRCore{})
		if cores.Error != nil {
			return DeleteWeekResult{}, fmt.Errorf("delete weekly report core data: %w", cores.Error)
		}
		result.Deleted.WeeklyCores = cores.RowsAffected
	}

	batches := s.db.WithContext(ctx).Where("quarter = ? AND week = ?", quarter, week).Delete(&domain.ReminderBatch{})
	if batches.Error != nil {
		return DeleteWeekResult{}, fmt.Errorf("delete weekly report reminder batches: %w", batches.Error)
	}
	result.Deleted.ReminderBatches = batches.RowsAffected

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
