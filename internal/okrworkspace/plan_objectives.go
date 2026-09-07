package okrworkspace

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// PlanObjectiveWriteInput is the Plan equivalent of the KR definition write
// used by Review. The optimistic version is scoped to one objective, not the
// entire Plan.
type PlanObjectiveWriteInput struct {
	ExpectedVersion int32             `json:"expected_version"`
	Objective       PlanObjectiveView `json:"objective"`
	UpdatedBy       string            `json:"-"`
}

type PlanObjectiveDeleteInput struct {
	ExpectedVersion int32 `json:"expected_version"`
}

func (s *Service) CreatePlanObjective(ctx context.Context, planID string, objective PlanObjectiveView, actor string) (PlanView, error) {
	planID = strings.TrimSpace(planID)
	actor = strings.TrimSpace(actor)
	if planID == "" {
		return PlanView{}, fmt.Errorf("plan id is required")
	}
	var plan domain.OKRPlan
	if err := s.db.WithContext(ctx).First(&plan, "id = ?", planID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return PlanView{}, ErrNotFound
		}
		return PlanView{}, fmt.Errorf("get plan for objective creation: %w", err)
	}
	normalized, err := normalizePlanContent(PlanContentView{Objectives: []PlanObjectiveView{objective}})
	if err != nil {
		return PlanView{}, err
	}
	objective = normalized.Objectives[0]
	if objective.ID == "" {
		return PlanView{}, fmt.Errorf("objective id is required")
	}
	var existing domain.Objective
	err = s.db.WithContext(ctx).First(&existing, "id = ?", objective.ID).Error
	if err == nil {
		if existing.PlanID == planID {
			return s.GetPlan(ctx, planID)
		}
		return PlanView{}, fmt.Errorf("objective id %s already exists", objective.ID)
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return PlanView{}, fmt.Errorf("check plan objective: %w", err)
	}
	var maxSort int
	if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Where("plan_id = ?", planID).Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSort).Error; err != nil {
		return PlanView{}, fmt.Errorf("get plan objective sort order: %w", err)
	}
	now := time.Now().UTC()
	row := domain.Objective{ID: objective.ID, PlanID: planID, Quarter: plan.Quarter, Title: objective.Title, SortOrder: maxSort + 1, CreatedAt: now, UpdatedAt: now}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return PlanView{}, fmt.Errorf("create plan objective: %w", err)
	}
	if err := s.writePlanObjectiveChildren(ctx, objective, actor, now); err != nil {
		return PlanView{}, err
	}
	if err := s.bumpPlan(ctx, planID, actor); err != nil {
		return PlanView{}, err
	}
	return s.GetPlan(ctx, planID)
}

func (s *Service) UpdatePlanObjective(ctx context.Context, planID, objectiveID string, input PlanObjectiveWriteInput) (PlanView, error) {
	planID, objectiveID = strings.TrimSpace(planID), strings.TrimSpace(objectiveID)
	input.UpdatedBy = strings.TrimSpace(input.UpdatedBy)
	if planID == "" || objectiveID == "" {
		return PlanView{}, fmt.Errorf("plan id and objective id are required")
	}
	if input.ExpectedVersion < 0 {
		return PlanView{}, fmt.Errorf("expected_version must be non-negative")
	}
	input.Objective.ID = objectiveID
	normalized, err := normalizePlanContent(PlanContentView{Objectives: []PlanObjectiveView{input.Objective}})
	if err != nil {
		return PlanView{}, err
	}
	objective := normalized.Objectives[0]
	current, err := s.planObjective(ctx, planID, objectiveID)
	if err != nil {
		return PlanView{}, err
	}
	result := s.db.WithContext(ctx).Model(&domain.Objective{}).
		Where("id = ? AND plan_id = ? AND version = ?", objectiveID, planID, input.ExpectedVersion).
		Updates(map[string]any{"title": objective.Title, "version": gorm.Expr("version + 1"), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return PlanView{}, fmt.Errorf("update plan objective: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return PlanView{}, ErrConflict
	}
	if err := s.deletePlanObjectiveChildren(ctx, current.ID); err != nil {
		return PlanView{}, err
	}
	if err := s.writePlanObjectiveChildren(ctx, objective, input.UpdatedBy, time.Now().UTC()); err != nil {
		return PlanView{}, err
	}
	if err := s.bumpPlan(ctx, planID, input.UpdatedBy); err != nil {
		return PlanView{}, err
	}
	return s.GetPlan(ctx, planID)
}

func (s *Service) DeletePlanObjective(ctx context.Context, planID, objectiveID string, input PlanObjectiveDeleteInput, actor string) error {
	planID, objectiveID = strings.TrimSpace(planID), strings.TrimSpace(objectiveID)
	if planID == "" || objectiveID == "" || input.ExpectedVersion < 0 {
		return fmt.Errorf("plan id, objective id and non-negative expected_version are required")
	}
	current, err := s.planObjective(ctx, planID, objectiveID)
	if err != nil {
		return err
	}
	// Check the objective version before touching any child rows. A stale
	// delete must never remove the latest editor's KR/metric/point definitions.
	if current.Version != input.ExpectedVersion {
		return ErrConflict
	}
	if err := s.deletePlanObjectiveChildren(ctx, objectiveID); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("id = ? AND plan_id = ? AND version = ?", objectiveID, planID, input.ExpectedVersion).Delete(&domain.Objective{})
	if result.Error != nil {
		return fmt.Errorf("delete plan objective: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	return s.bumpPlan(ctx, planID, actor)
}

func (s *Service) ReorderPlanObjectives(ctx context.Context, planID string, ids []string, actor string) error {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return fmt.Errorf("plan id is required")
	}
	var records []domain.Objective
	if err := s.db.WithContext(ctx).Where("plan_id = ?", planID).Find(&records).Error; err != nil {
		return fmt.Errorf("list plan objectives for reorder: %w", err)
	}
	known := make(map[string]struct{}, len(records))
	for _, record := range records {
		known[record.ID] = struct{}{}
	}
	ordered, err := completeOrder(ids, known, fmt.Sprintf("plan %s", planID))
	if err != nil {
		return err
	}
	for index, id := range ordered {
		if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Where("id = ? AND plan_id = ?", id, planID).Updates(map[string]any{"sort_order": index, "updated_at": time.Now().UTC()}).Error; err != nil {
			return fmt.Errorf("write plan objective order: %w", err)
		}
	}
	return s.bumpPlan(ctx, planID, actor)
}

func (s *Service) bumpPlan(ctx context.Context, planID, actor string) error {
	content, err := s.planContentRows(ctx, planID)
	if err != nil {
		return err
	}
	encoded, err := encodePlanContent(content)
	if err != nil {
		return err
	}
	updates := map[string]any{"version": gorm.Expr("version + 1"), "updated_at": time.Now().UTC()}
	updates["content"] = encoded
	if strings.TrimSpace(actor) != "" {
		updates["updated_by"] = strings.TrimSpace(actor)
	}
	if err := s.db.WithContext(ctx).Model(&domain.OKRPlan{}).Where("id = ?", planID).Updates(updates).Error; err != nil {
		return fmt.Errorf("update plan metadata: %w", err)
	}
	return nil
}
