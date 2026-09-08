package okrworkspace

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/okrworkspace/domain"
)

// ReplaceKRDefinitionInput carries only the wording and the people of a KR and
// of its existing concrete points. The array order is the canonical point order
// when the request contains the complete existing set. Metrics, labels and the
// set of points have no representation here, so a week-scoped page can edit the
// shared definition without any of its weekly values reaching the definition.
type ReplaceKRDefinitionInput struct {
	ExpectedVersion int32                 `json:"expected_version"`
	Title           string                `json:"title"`
	Owners          []OwnerView           `json:"owners"`
	Points          []PointDefinitionView `json:"points"`
	UpdatedBy       string                `json:"-"`
}

// PointDefinitionView names one existing point to retitle or reassign. Points
// left out of the request keep their current wording, people and order.
type PointDefinitionView struct {
	ID     string      `json:"id"`
	Title  string      `json:"title"`
	Owners []OwnerView `json:"owners"`
}

func (s *Service) ReplaceKRDefinition(ctx context.Context, id string, input ReplaceKRDefinitionInput) (KRView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return KRView{}, fmt.Errorf("kr_id is required")
	}
	if input.ExpectedVersion < 0 {
		return KRView{}, fmt.Errorf("expected_version must be non-negative")
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return KRView{}, fmt.Errorf("kr title is required")
	}
	db := s.db.WithContext(ctx)
	var records []domain.KRPoint
	if err := db.Where("kr_id = ?", id).Find(&records).Error; err != nil {
		return KRView{}, fmt.Errorf("list points for definition update: %w", err)
	}
	owned := make(map[string]struct{}, len(records))
	for _, record := range records {
		owned[record.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(input.Points))
	for index := range input.Points {
		point := &input.Points[index]
		point.ID = strings.TrimSpace(point.ID)
		point.Title = strings.TrimSpace(point.Title)
		if point.ID == "" || point.Title == "" {
			return KRView{}, fmt.Errorf("points require an id and a title")
		}
		if _, ok := owned[point.ID]; !ok {
			return KRView{}, fmt.Errorf("point %s does not belong to KR %s; this endpoint cannot add or remove points", point.ID, id)
		}
		if _, duplicate := seen[point.ID]; duplicate {
			return KRView{}, fmt.Errorf("point %s appears twice", point.ID)
		}
		seen[point.ID] = struct{}{}
	}
	completePointOrder := len(input.Points) == len(records)
	if err := updateKRVersion(db, id, input.ExpectedVersion, map[string]any{
		"title": input.Title, "updated_by": input.UpdatedBy,
	}); err != nil {
		return KRView{}, err
	}
	if err := replaceKROwners(db, id, normalizeOwners(input.Owners)); err != nil {
		return KRView{}, err
	}
	for index, point := range input.Points {
		updates := map[string]any{"title": point.Title}
		if completePointOrder {
			updates["sort_order"] = index
		}
		if err := db.Model(&domain.KRPoint{}).Where("id = ? AND kr_id = ?", point.ID, id).Updates(updates).Error; err != nil {
			return KRView{}, fmt.Errorf("update point title: %w", err)
		}
		if err := replacePointOwners(db, point.ID, normalizeOwners(point.Owners)); err != nil {
			return KRView{}, err
		}
	}
	return s.GetCoreKR(ctx, id)
}
