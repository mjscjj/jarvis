package okrworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// ReplaceKRTagsInput exposes only labels, with the same KR version used by
// definition edits. It cannot replace owners, metrics, points or weekly data.
type ReplaceKRTagsInput struct {
	ExpectedVersion int32     `json:"expected_version"`
	Tags            []TagView `json:"tags"`
	UpdatedBy       string    `json:"-"`
}

// ReplacePointTagsInput changes labels on one concrete strategy/product point.
// The parent KR version is the conflict boundary shared with definition edits.
type ReplacePointTagsInput struct {
	ExpectedVersion int32     `json:"expected_version"`
	Tags            []TagView `json:"tags"`
	UpdatedBy       string    `json:"-"`
}

func (s *Service) ReplaceKRTags(ctx context.Context, id string, input ReplaceKRTagsInput) (KRView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return KRView{}, fmt.Errorf("kr_id is required")
	}
	if input.ExpectedVersion < 0 {
		return KRView{}, fmt.Errorf("expected_version must be non-negative")
	}
	if input.Tags == nil {
		return KRView{}, fmt.Errorf("tags is required; use [] to clear all tags")
	}
	tags := normalizeTags(input.Tags)
	if err := validateTags(tags); err != nil {
		return KRView{}, err
	}
	db := s.db.WithContext(ctx)
	if err := updateKRVersion(db, id, input.ExpectedVersion, map[string]any{"updated_by": input.UpdatedBy}); err != nil {
		return KRView{}, err
	}
	if err := db.Where("kr_id = ?", id).Delete(&domain.KRTag{}).Error; err != nil {
		return KRView{}, fmt.Errorf("replace KR tags: %w", err)
	}
	for _, tag := range tags {
		if err := db.Create(&domain.KRTag{KRID: id, Type: tag.Type, Value: tag.Value}).Error; err != nil {
			return KRView{}, fmt.Errorf("create KR tag: %w", err)
		}
	}
	return s.GetCoreKR(ctx, id)
}

func (s *Service) GetCoreKRByPointID(ctx context.Context, pointID string) (KRView, error) {
	pointID = strings.TrimSpace(pointID)
	if pointID == "" {
		return KRView{}, fmt.Errorf("point_id is required")
	}
	var point domain.KRPoint
	if err := s.db.WithContext(ctx).First(&point, "id = ?", pointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get point: %w", err)
	}
	return s.GetCoreKR(ctx, point.KRID)
}

func (s *Service) ReplacePointTags(ctx context.Context, pointID string, input ReplacePointTagsInput) (KRView, error) {
	pointID = strings.TrimSpace(pointID)
	if pointID == "" {
		return KRView{}, fmt.Errorf("point_id is required")
	}
	if input.ExpectedVersion < 0 {
		return KRView{}, fmt.Errorf("expected_version must be non-negative")
	}
	if input.Tags == nil {
		return KRView{}, fmt.Errorf("tags is required; use [] to clear all tags")
	}
	tags := normalizeTags(input.Tags)
	if err := validatePointTags(tags); err != nil {
		return KRView{}, err
	}
	db := s.db.WithContext(ctx)
	var point domain.KRPoint
	if err := db.First(&point, "id = ?", pointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get point for tag replacement: %w", err)
	}
	if err := updateKRVersion(db, point.KRID, input.ExpectedVersion, map[string]any{"updated_by": input.UpdatedBy}); err != nil {
		return KRView{}, err
	}
	if err := db.Where("point_id = ?", pointID).Delete(&domain.PointTag{}).Error; err != nil {
		return KRView{}, fmt.Errorf("replace point tags: %w", err)
	}
	for _, tag := range tags {
		if err := db.Create(&domain.PointTag{PointID: pointID, Type: tag.Type, Value: tag.Value}).Error; err != nil {
			return KRView{}, fmt.Errorf("create point tag: %w", err)
		}
	}
	return s.GetCoreKR(ctx, point.KRID)
}
