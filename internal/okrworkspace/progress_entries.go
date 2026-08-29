package okrworkspace

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// ProgressEntryInput is the narrow Agent-facing write contract for one weekly
// progress record. It deliberately contains no OKR definition fields.
type ProgressEntryInput struct {
	ID              string            `json:"id"`
	ExpectedVersion int32             `json:"expected_version"`
	Week            string            `json:"week"`
	Status          domain.Status     `json:"status"`
	Text            string            `json:"text"`
	Docs            []domain.DocLink  `json:"docs"`
	Images          []domain.ImageRef `json:"images"`
	Source          string            `json:"source"`
	NeedsReview     bool              `json:"needs_review"`
	UpdatedBy       string            `json:"updated_by"`
}

type DeleteProgressEntryInput struct {
	ExpectedVersion int32  `json:"expected_version"`
	UpdatedBy       string `json:"updated_by"`
}

func (s *Service) GetKRByProgress(ctx context.Context, progressID string) (KRView, error) {
	row, _, kr, _, err := s.progressEntryOwner(ctx, strings.TrimSpace(progressID))
	if err != nil {
		return KRView{}, err
	}
	return s.GetKR(ctx, kr.ID, row.Week)
}

func (s *Service) CreateProgressEntry(ctx context.Context, pointID string, input ProgressEntryInput) (KRView, error) {
	pointID = strings.TrimSpace(pointID)
	input.ID = strings.TrimSpace(input.ID)
	if pointID == "" || input.ID == "" {
		return KRView{}, fmt.Errorf("point_id and progress id are required")
	}
	if err := validateProgressEntryInput(input); err != nil {
		return KRView{}, err
	}
	point, kr, objective, err := s.progressPointOwner(ctx, pointID)
	if err != nil {
		return KRView{}, err
	}
	if err := s.requireOpenWeek(ctx, objective.Quarter, input.Week); err != nil {
		return KRView{}, err
	}

	var existing domain.KRProgress
	err = s.db.WithContext(ctx).First(&existing, "id = ?", input.ID).Error
	if err == nil {
		if sameProgressEntry(existing, point.ID, input) {
			return s.GetKR(ctx, kr.ID, input.Week)
		}
		return KRView{}, fmt.Errorf("progress id %s already exists with different content", input.ID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return KRView{}, fmt.Errorf("check progress id: %w", err)
	}

	if err := updateKRVersion(s.db.WithContext(ctx), kr.ID, input.ExpectedVersion, map[string]any{"updated_by": input.UpdatedBy}); err != nil {
		return KRView{}, err
	}
	var maxSort int
	if err := s.db.WithContext(ctx).Model(&domain.KRProgress{}).Where("point_id = ? AND week = ?", point.ID, input.Week).
		Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSort).Error; err != nil {
		return KRView{}, fmt.Errorf("get progress sort order: %w", err)
	}
	row := domain.KRProgress{
		ID: input.ID, PointID: point.ID, Week: input.Week, Status: input.Status, Text: strings.TrimSpace(input.Text),
		Docs: nonNilDocs(input.Docs), Images: nonNilImages(input.Images), Source: normalizedSource(input.Source),
		NeedsReview: input.NeedsReview, SortOrder: maxSort + 1, CreatedBy: input.UpdatedBy, UpdatedBy: input.UpdatedBy,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return KRView{}, fmt.Errorf("create progress entry: %w", err)
	}
	return s.GetKR(ctx, kr.ID, input.Week)
}

func (s *Service) UpdateProgressEntry(ctx context.Context, progressID string, input ProgressEntryInput) (KRView, error) {
	progressID = strings.TrimSpace(progressID)
	if progressID == "" {
		return KRView{}, fmt.Errorf("progress_id is required")
	}
	if input.ID != "" && strings.TrimSpace(input.ID) != progressID {
		return KRView{}, fmt.Errorf("progress id cannot be changed")
	}
	input.ID = progressID
	if err := validateProgressEntryInput(input); err != nil {
		return KRView{}, err
	}
	row, point, kr, objective, err := s.progressEntryOwner(ctx, progressID)
	if err != nil {
		return KRView{}, err
	}
	if row.Week != input.Week {
		return KRView{}, fmt.Errorf("progress week cannot be changed")
	}
	if err := s.requireOpenWeek(ctx, objective.Quarter, input.Week); err != nil {
		return KRView{}, err
	}
	if err := updateKRVersion(s.db.WithContext(ctx), kr.ID, input.ExpectedVersion, map[string]any{"updated_by": input.UpdatedBy}); err != nil {
		return KRView{}, err
	}
	updates := map[string]any{
		"status": input.Status, "text": strings.TrimSpace(input.Text), "docs": nonNilDocs(input.Docs),
		"images": nonNilImages(input.Images), "source": normalizedSource(input.Source),
		"needs_review": input.NeedsReview, "updated_by": input.UpdatedBy,
	}
	result := s.db.WithContext(ctx).Model(&domain.KRProgress{}).Where("id = ? AND point_id = ?", progressID, point.ID).Updates(updates)
	if result.Error != nil {
		return KRView{}, fmt.Errorf("update progress entry: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return KRView{}, ErrNotFound
	}
	return s.GetKR(ctx, kr.ID, input.Week)
}

func (s *Service) DeleteProgressEntry(ctx context.Context, progressID string, input DeleteProgressEntryInput) (KRView, error) {
	progressID = strings.TrimSpace(progressID)
	if progressID == "" {
		return KRView{}, fmt.Errorf("progress_id is required")
	}
	if input.ExpectedVersion < 0 {
		return KRView{}, fmt.Errorf("expected_version must be non-negative")
	}
	row, _, kr, objective, err := s.progressEntryOwner(ctx, progressID)
	if err != nil {
		return KRView{}, err
	}
	if err := s.requireOpenWeek(ctx, objective.Quarter, row.Week); err != nil {
		return KRView{}, err
	}
	if err := updateKRVersion(s.db.WithContext(ctx), kr.ID, input.ExpectedVersion, map[string]any{"updated_by": input.UpdatedBy}); err != nil {
		return KRView{}, err
	}
	if err := s.db.WithContext(ctx).Where("target_id = ?", progressID).Delete(&domain.PageComment{}).Error; err != nil {
		return KRView{}, fmt.Errorf("delete progress comments: %w", err)
	}
	result := s.db.WithContext(ctx).Where("id = ?", progressID).Delete(&domain.KRProgress{})
	if result.Error != nil {
		return KRView{}, fmt.Errorf("delete progress entry: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return KRView{}, ErrNotFound
	}
	return s.GetKR(ctx, kr.ID, row.Week)
}

func validateProgressEntryInput(input ProgressEntryInput) error {
	if input.ExpectedVersion < 0 {
		return fmt.Errorf("expected_version must be non-negative")
	}
	if !weekPattern.MatchString(strings.TrimSpace(input.Week)) {
		return fmt.Errorf("week must use YYYY-Www")
	}
	if !domain.ValidStatus(input.Status) || strings.TrimSpace(input.Text) == "" {
		return fmt.Errorf("progress status and text are required")
	}
	return nil
}

func (s *Service) progressPointOwner(ctx context.Context, pointID string) (domain.KRPoint, domain.KR, domain.Objective, error) {
	var point domain.KRPoint
	if err := s.db.WithContext(ctx).First(&point, "id = ?", pointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.KRPoint{}, domain.KR{}, domain.Objective{}, ErrNotFound
		}
		return domain.KRPoint{}, domain.KR{}, domain.Objective{}, fmt.Errorf("get progress point: %w", err)
	}
	var kr domain.KR
	if err := s.db.WithContext(ctx).First(&kr, "id = ?", point.KRID).Error; err != nil {
		return domain.KRPoint{}, domain.KR{}, domain.Objective{}, fmt.Errorf("get progress kr: %w", err)
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ?", kr.ObjectiveID).Error; err != nil {
		return domain.KRPoint{}, domain.KR{}, domain.Objective{}, fmt.Errorf("get progress objective: %w", err)
	}
	return point, kr, objective, nil
}

func (s *Service) krObjective(ctx context.Context, krID string) (domain.KR, domain.Objective, error) {
	var kr domain.KR
	if err := s.db.WithContext(ctx).First(&kr, "id = ?", strings.TrimSpace(krID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.KR{}, domain.Objective{}, ErrNotFound
		}
		return domain.KR{}, domain.Objective{}, fmt.Errorf("get weekly report kr: %w", err)
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ?", kr.ObjectiveID).Error; err != nil {
		return domain.KR{}, domain.Objective{}, fmt.Errorf("get weekly report objective: %w", err)
	}
	return kr, objective, nil
}

func (s *Service) progressEntryOwner(ctx context.Context, progressID string) (domain.KRProgress, domain.KRPoint, domain.KR, domain.Objective, error) {
	var row domain.KRProgress
	if err := s.db.WithContext(ctx).First(&row, "id = ?", progressID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.KRProgress{}, domain.KRPoint{}, domain.KR{}, domain.Objective{}, ErrNotFound
		}
		return domain.KRProgress{}, domain.KRPoint{}, domain.KR{}, domain.Objective{}, fmt.Errorf("get progress entry: %w", err)
	}
	point, kr, objective, err := s.progressPointOwner(ctx, row.PointID)
	return row, point, kr, objective, err
}

func sameProgressEntry(row domain.KRProgress, pointID string, input ProgressEntryInput) bool {
	return row.PointID == pointID && row.Week == input.Week && row.Status == input.Status &&
		row.Text == strings.TrimSpace(input.Text) && reflect.DeepEqual(nonNilDocs(row.Docs), nonNilDocs(input.Docs)) &&
		reflect.DeepEqual(nonNilImages(row.Images), nonNilImages(input.Images)) && row.Source == normalizedSource(input.Source) &&
		row.NeedsReview == input.NeedsReview
}
