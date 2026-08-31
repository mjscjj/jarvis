package okrworkspace

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/okrworkspace/domain"
)

// ReplaceKRTagsInput exposes only labels, with the same KR version used by
// definition edits. It cannot replace owners, metrics, points or weekly data.
type ReplaceKRTagsInput struct {
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
