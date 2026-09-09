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

// PatchPointDefinitionInput changes only fields owned by one concrete KR row.
// Pointers distinguish an omitted field from an explicit empty owners list.
type PatchPointDefinitionInput struct {
	Title     *string      `json:"title"`
	Owners    *[]OwnerView `json:"owners"`
	UpdatedBy string       `json:"-"`
}

// PointDefinitionPatchResult carries only the versions invalidated by the
// narrow write. Callers keep their local draft instead of replacing a whole KR
// or Plan with a response assembled while somebody else may still be editing.
type PointDefinitionPatchResult struct {
	PointID          string `json:"point_id"`
	KRID             string `json:"kr_id"`
	KRVersion        int32  `json:"kr_version"`
	ObjectiveID      string `json:"objective_id"`
	ObjectiveVersion int32  `json:"objective_version"`
	PlanID           string `json:"plan_id,omitempty"`
	PlanVersion      int32  `json:"plan_version,omitempty"`
}

// PatchPointDefinition updates one point in the committed OKR definition used
// by Review and weekly reports. Plan draft points use PatchPlanPointDefinition.
func (s *Service) PatchPointDefinition(ctx context.Context, pointID string, input PatchPointDefinitionInput) (PointDefinitionPatchResult, error) {
	return s.patchPointDefinition(ctx, "", pointID, input)
}

// PatchPlanPointDefinition updates one point in one Plan without replacing the
// other KRs and points in its Objective.
func (s *Service) PatchPlanPointDefinition(ctx context.Context, planID, pointID string, input PatchPointDefinitionInput) (PointDefinitionPatchResult, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return PointDefinitionPatchResult{}, fmt.Errorf("plan_id is required")
	}
	return s.patchPointDefinition(ctx, planID, pointID, input)
}

func (s *Service) patchPointDefinition(ctx context.Context, planID, pointID string, input PatchPointDefinitionInput) (PointDefinitionPatchResult, error) {
	pointID = strings.TrimSpace(pointID)
	input.UpdatedBy = strings.TrimSpace(input.UpdatedBy)
	if pointID == "" {
		return PointDefinitionPatchResult{}, fmt.Errorf("point_id is required")
	}
	if input.Title == nil && input.Owners == nil {
		return PointDefinitionPatchResult{}, fmt.Errorf("title or owners is required")
	}
	if input.Title != nil {
		clean := strings.TrimSpace(*input.Title)
		if clean == "" && planID == "" {
			return PointDefinitionPatchResult{}, fmt.Errorf("point title is required")
		}
		input.Title = &clean
	}
	if input.Owners != nil {
		normalized := normalizeOwners(*input.Owners)
		input.Owners = &normalized
	}

	db := s.db.WithContext(ctx)
	var point domain.KRPoint
	if err := db.First(&point, "id = ?", pointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PointDefinitionPatchResult{}, ErrNotFound
		}
		return PointDefinitionPatchResult{}, fmt.Errorf("get point definition: %w", err)
	}
	var kr domain.KR
	if err := db.First(&kr, "id = ?", point.KRID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PointDefinitionPatchResult{}, ErrNotFound
		}
		return PointDefinitionPatchResult{}, fmt.Errorf("get point KR: %w", err)
	}
	var objective domain.Objective
	if err := db.First(&objective, "id = ?", kr.ObjectiveID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PointDefinitionPatchResult{}, ErrNotFound
		}
		return PointDefinitionPatchResult{}, fmt.Errorf("get point objective: %w", err)
	}
	if objective.PlanID != planID {
		return PointDefinitionPatchResult{}, ErrNotFound
	}

	if input.Title != nil {
		result := db.Model(&domain.KRPoint{}).Where("id = ? AND kr_id = ?", point.ID, kr.ID).Update("title", *input.Title)
		if result.Error != nil {
			return PointDefinitionPatchResult{}, fmt.Errorf("update point title: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return PointDefinitionPatchResult{}, ErrNotFound
		}
	}
	if input.Owners != nil {
		if err := replacePointOwners(db, point.ID, *input.Owners); err != nil {
			return PointDefinitionPatchResult{}, err
		}
	}

	now := time.Now().UTC()
	krUpdates := map[string]any{"version": gorm.Expr("version + 1"), "updated_at": now}
	if input.UpdatedBy != "" {
		krUpdates["updated_by"] = input.UpdatedBy
	}
	bumpedKR := domain.KR{}
	krResult := db.Model(&bumpedKR).Clauses(clause.Returning{Columns: []clause.Column{{Name: "version"}}}).Where("id = ?", kr.ID).Updates(krUpdates)
	if krResult.Error != nil {
		return PointDefinitionPatchResult{}, fmt.Errorf("bump point KR version: %w", krResult.Error)
	}
	if krResult.RowsAffected != 1 {
		return PointDefinitionPatchResult{}, ErrNotFound
	}
	result := PointDefinitionPatchResult{PointID: point.ID, KRID: kr.ID, KRVersion: bumpedKR.Version, ObjectiveID: objective.ID, PlanID: planID}
	if planID != "" {
		bumpedObjective := domain.Objective{}
		objectiveResult := db.Model(&bumpedObjective).Clauses(clause.Returning{Columns: []clause.Column{{Name: "version"}}}).Where("id = ? AND plan_id = ?", objective.ID, planID).Updates(map[string]any{
			"version": gorm.Expr("version + 1"), "updated_at": now,
		})
		if objectiveResult.Error != nil {
			return PointDefinitionPatchResult{}, fmt.Errorf("bump plan objective version: %w", objectiveResult.Error)
		}
		if objectiveResult.RowsAffected != 1 {
			return PointDefinitionPatchResult{}, ErrNotFound
		}
		result.ObjectiveVersion = bumpedObjective.Version

		planUpdates := map[string]any{"version": gorm.Expr("version + 1"), "updated_at": now}
		if input.UpdatedBy != "" {
			planUpdates["updated_by"] = input.UpdatedBy
		}
		bumpedPlan := domain.OKRPlan{}
		planResult := db.Model(&bumpedPlan).Clauses(clause.Returning{Columns: []clause.Column{{Name: "version"}}}).Where("id = ?", planID).Updates(planUpdates)
		if planResult.Error != nil {
			return PointDefinitionPatchResult{}, fmt.Errorf("bump point plan version: %w", planResult.Error)
		}
		if planResult.RowsAffected != 1 {
			return PointDefinitionPatchResult{}, ErrNotFound
		}
		result.PlanVersion = bumpedPlan.Version
	} else {
		result.ObjectiveVersion = objective.Version
	}
	return result, nil
}
