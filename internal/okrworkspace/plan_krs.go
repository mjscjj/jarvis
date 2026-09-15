package okrworkspace

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// PlanKRWriteInput replaces the definition and point structure owned by one
// KR. Existing point wording and owners remain owned by the point endpoint.
type PlanKRWriteInput struct {
	ExpectedVersion        int32      `json:"expected_version"`
	ExpectedStructureToken string     `json:"expected_structure_token"`
	KR                     PlanKRView `json:"kr"`
	UpdatedBy              string     `json:"-"`
}

// UpdatePlanKR saves one KR without incrementing or comparing its parent
// Objective version. Sibling KRs therefore remain independently editable.
func (s *Service) UpdatePlanKR(ctx context.Context, planID, krID string, input PlanKRWriteInput) (PlanView, error) {
	planID, krID = strings.TrimSpace(planID), strings.TrimSpace(krID)
	input.UpdatedBy = strings.TrimSpace(input.UpdatedBy)
	if planID == "" || krID == "" {
		return PlanView{}, fmt.Errorf("plan id and kr id are required")
	}
	if input.ExpectedVersion < 0 {
		return PlanView{}, fmt.Errorf("expected_version must be non-negative")
	}
	currentNode, err := s.GetPlanKRNode(ctx, krID)
	if err != nil {
		return PlanView{}, err
	}
	if currentNode.PlanID != planID {
		return PlanView{}, ErrNotFound
	}

	input.KR.ID = krID
	currentPoints := make(map[string]PlanPointView, len(currentNode.KR.Points))
	for _, point := range currentNode.KR.Points {
		currentPoints[point.ID] = point
	}
	for index := range input.KR.Points {
		point := &input.KR.Points[index]
		if saved, exists := currentPoints[point.ID]; exists {
			// Existing point content is never accepted from a parent KR snapshot.
			// The aggregate write owns only point membership and ordering.
			*point = saved
		}
	}
	wrapper, err := normalizePlanObjectives([]PlanObjectiveView{{
		ID: currentNode.ObjectiveID, Title: currentNode.ObjectiveTitle, KRs: []PlanKRView{input.KR},
	}})
	if err != nil {
		return PlanView{}, err
	}
	incoming := wrapper[0].KRs[0]
	if planKRRemovesChildren(currentNode.KR, incoming) && currentNode.KR.StructureToken != strings.TrimSpace(input.ExpectedStructureToken) {
		return PlanView{}, ErrConflict
	}
	if err := s.verifyPlanPeople(ctx, wrapper); err != nil {
		return PlanView{}, err
	}

	now := time.Now().UTC()
	db := s.db.WithContext(ctx)
	result := db.Model(&domain.KR{}).
		Where("id = ? AND objective_id = ? AND version = ?", krID, currentNode.ObjectiveID, input.ExpectedVersion).
		Updates(map[string]any{
			"title": incoming.Title, "metric_note": incoming.MetricNote,
			"version": gorm.Expr("version + 1"), "updated_at": now, "updated_by": input.UpdatedBy,
		})
	if result.Error != nil {
		return PlanView{}, fmt.Errorf("update plan KR: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return PlanView{}, ErrConflict
	}
	if err := s.replacePlanKRChildren(db, incoming); err != nil {
		return PlanView{}, err
	}
	if err := s.bumpPlan(ctx, planID, input.UpdatedBy); err != nil {
		return PlanView{}, err
	}
	return s.GetPlan(ctx, planID)
}

func (s *Service) replacePlanKRChildren(db *gorm.DB, kr PlanKRView) error {
	if err := replaceKROwners(db, kr.ID, normalizeOwners(kr.Owners)); err != nil {
		return err
	}
	if err := db.Where("kr_id = ?", kr.ID).Delete(&domain.KRMetric{}).Error; err != nil {
		return fmt.Errorf("replace plan KR metrics: %w", err)
	}
	for index, metric := range kr.Metrics {
		if err := db.Create(&domain.KRMetric{ID: metric.ID, KRID: kr.ID, Text: metric.Text, Light: metric.Light, Images: nonNilImages(metric.Images), SortOrder: index}).Error; err != nil {
			return fmt.Errorf("create plan metric %s: %w", metric.ID, err)
		}
	}
	if err := db.Where("kr_id = ?", kr.ID).Delete(&domain.KRTag{}).Error; err != nil {
		return fmt.Errorf("replace plan KR tags: %w", err)
	}
	for _, tag := range kr.Tags {
		if err := db.Create(&domain.KRTag{KRID: kr.ID, Type: tag.Type, Value: tag.Value}).Error; err != nil {
			return fmt.Errorf("create plan KR tag: %w", err)
		}
	}

	var existingPoints []domain.KRPoint
	if err := db.Where("kr_id = ?", kr.ID).Find(&existingPoints).Error; err != nil {
		return fmt.Errorf("list existing plan points: %w", err)
	}
	existingPointIDs := make(map[string]struct{}, len(existingPoints))
	for _, point := range existingPoints {
		existingPointIDs[point.ID] = struct{}{}
	}
	incomingPointIDs := make(map[string]struct{}, len(kr.Points))
	for index, point := range kr.Points {
		incomingPointIDs[point.ID] = struct{}{}
		if _, exists := existingPointIDs[point.ID]; exists {
			if err := db.Model(&domain.KRPoint{}).Where("id = ? AND kr_id = ?", point.ID, kr.ID).Update("sort_order", index).Error; err != nil {
				return fmt.Errorf("update plan point order: %w", err)
			}
			continue
		}
		record := domain.KRPoint{
			ID: point.ID, KRID: kr.ID, Version: point.Version, Kind: point.Kind, Title: point.Title,
			MeegoWorkItemID: strings.TrimSpace(point.MeegoWorkItemID), MeegoURL: strings.TrimSpace(point.MeegoURL), SortOrder: index,
		}
		if err := db.Create(&record).Error; err != nil {
			return fmt.Errorf("create plan point %s: %w", point.ID, err)
		}
		if err := replacePointOwners(db, point.ID, normalizeOwners(point.Owners)); err != nil {
			return err
		}
		for _, tag := range point.Tags {
			if err := db.Create(&domain.PointTag{PointID: point.ID, Type: tag.Type, Value: tag.Value}).Error; err != nil {
				return fmt.Errorf("create plan point tag: %w", err)
			}
		}
	}
	var removedPointIDs []string
	for _, point := range existingPoints {
		if _, kept := incomingPointIDs[point.ID]; !kept {
			removedPointIDs = append(removedPointIDs, point.ID)
		}
	}
	return purgePoints(db, removedPointIDs, true)
}
