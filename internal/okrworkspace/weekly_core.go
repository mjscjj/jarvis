package okrworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// WeeklyKRCoreInput is the reusable OKR write contract for one week's core
// data shown under one KR. It cannot change the KR title, owners, tags or
// decomposition definitions. When neither the definition nor this week has a
// metric yet, the week may seed its own first metric row; later writes preserve
// that week's metric IDs.
type WeeklyKRCoreInput struct {
	ExpectedVersion int32        `json:"expected_version"`
	Week            string       `json:"week"`
	MetricNote      string       `json:"metric_note"`
	Metrics         []MetricView `json:"metrics"`
	UpdatedBy       string       `json:"updated_by"`
}

func (s *Service) ReplaceWeeklyKRCore(ctx context.Context, krID string, input WeeklyKRCoreInput) (KRView, error) {
	krID = strings.TrimSpace(krID)
	input.Week = strings.TrimSpace(input.Week)
	if krID == "" {
		return KRView{}, fmt.Errorf("kr_id is required")
	}
	if input.ExpectedVersion < 0 {
		return KRView{}, fmt.Errorf("expected_version must be non-negative")
	}
	if !weekPattern.MatchString(input.Week) {
		return KRView{}, fmt.Errorf("week must use YYYY-Www")
	}
	_, objective, err := s.krObjective(ctx, krID)
	if err != nil {
		return KRView{}, err
	}
	if err := s.requireOpenWeek(ctx, objective.Quarter, input.Week); err != nil {
		return KRView{}, err
	}

	var existing domain.WeeklyKRCore
	existingErr := s.db.WithContext(ctx).First(&existing, "kr_id = ? AND week = ?", krID, input.Week).Error
	if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		return KRView{}, fmt.Errorf("get weekly core data: %w", existingErr)
	}
	allowedIDs, err := s.weeklyMetricIDs(ctx, krID, existing)
	if err != nil {
		return KRView{}, err
	}
	if err := validateWeeklyMetrics(input.Metrics, allowedIDs); err != nil {
		return KRView{}, err
	}
	metrics := make([]domain.WeeklyMetric, 0, len(input.Metrics))
	for _, metric := range input.Metrics {
		metrics = append(metrics, domain.WeeklyMetric{
			ID: strings.TrimSpace(metric.ID), Text: metric.Text, Light: metric.Light, Images: nonNilImages(metric.Images),
		})
	}

	if errors.Is(existingErr, gorm.ErrRecordNotFound) {
		if input.ExpectedVersion != 0 {
			return KRView{}, ErrConflict
		}
		row := domain.WeeklyKRCore{
			KRID: krID, Week: input.Week, MetricNote: input.MetricNote, Metrics: metrics,
			CreatedBy: input.UpdatedBy, UpdatedBy: input.UpdatedBy,
		}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return KRView{}, fmt.Errorf("create weekly core data: %w", err)
		}
		return s.GetProgressKR(ctx, krID, input.Week)
	}

	result := s.db.WithContext(ctx).Model(&domain.WeeklyKRCore{}).
		Where("kr_id = ? AND week = ? AND version = ?", krID, input.Week, input.ExpectedVersion).
		Select("metric_note", "metrics", "updated_by", "version").
		Updates(domain.WeeklyKRCore{
			MetricNote: input.MetricNote, Metrics: metrics, UpdatedBy: input.UpdatedBy, Version: existing.Version + 1,
		})
	if result.Error != nil {
		return KRView{}, fmt.Errorf("update weekly core data: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return KRView{}, ErrConflict
	}
	return s.GetProgressKR(ctx, krID, input.Week)
}

func (s *Service) weeklyMetricIDs(ctx context.Context, krID string, existing domain.WeeklyKRCore) ([]string, error) {
	if existing.KRID != "" {
		ids := make([]string, 0, len(existing.Metrics))
		for _, metric := range existing.Metrics {
			ids = append(ids, metric.ID)
		}
		return ids, nil
	}
	var rows []domain.KRMetric
	if err := s.db.WithContext(ctx).Where("kr_id = ?", krID).Order("sort_order, id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list weekly core metric definitions: %w", err)
	}
	ids := make([]string, 0, len(rows))
	for _, metric := range rows {
		ids = append(ids, metric.ID)
	}
	return ids, nil
}

func validateWeeklyMetrics(metrics []MetricView, allowedIDs []string) error {
	if len(allowedIDs) == 0 {
		if len(metrics) > 1 {
			return fmt.Errorf("weekly core data can seed only one metric when the definition has none")
		}
		seen := make(map[string]struct{}, len(metrics))
		for _, metric := range metrics {
			id := strings.TrimSpace(metric.ID)
			if id == "" || !domain.ValidLight(metric.Light) || !weeklyMetricHasContent(metric) {
				return fmt.Errorf("weekly core data metrics require ids, text or images, and a valid light")
			}
			if _, exists := seen[id]; exists {
				return fmt.Errorf("weekly core data metrics require unique ids")
			}
			seen[id] = struct{}{}
		}
		return nil
	}
	if len(metrics) != len(allowedIDs) {
		return fmt.Errorf("weekly core data must preserve the metric definitions")
	}
	for index, metric := range metrics {
		if strings.TrimSpace(metric.ID) == "" || metric.ID != allowedIDs[index] || !domain.ValidLight(metric.Light) || !weeklyMetricHasContent(metric) {
			return fmt.Errorf("weekly core data must preserve metric ids and require text or images with a valid light")
		}
	}
	return nil
}

func weeklyMetricHasContent(metric MetricView) bool {
	return strings.TrimSpace(metric.Text) != "" || len(metric.Images) > 0
}
