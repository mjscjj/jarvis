package okrworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// WorldNodeView is the smallest read model needed by the cross-module world
// graph. The OKR module remains the source of truth for the payload.
type WorldNodeView struct {
	Name string
	Data any
}

// ResolveWorldNode reads a stable OKR or Biz OKR plan node from the module's
// existing definition tables. It neither copies the node nor infers edges.
func (s *Service) ResolveWorldNode(ctx context.Context, nodeType, id string) (WorldNodeView, error) {
	nodeType = strings.TrimSpace(nodeType)
	id = strings.TrimSpace(id)
	if id == "" {
		return WorldNodeView{}, fmt.Errorf("world node id is required")
	}
	switch nodeType {
	case "okr_objective":
		return s.resolveObjectiveNode(ctx, id, false)
	case "okr_kr":
		return s.resolveKRNode(ctx, id, false)
	case "okr_point":
		return s.resolvePointNode(ctx, id, false)
	case "biz_okr_plan_objective":
		return s.resolveObjectiveNode(ctx, id, true)
	case "biz_okr_plan_kr":
		return s.resolveKRNode(ctx, id, true)
	case "biz_okr_plan_point":
		return s.resolvePointNode(ctx, id, true)
	default:
		return WorldNodeView{}, fmt.Errorf("unsupported OKR world node type %q", nodeType)
	}
}

func (s *Service) resolveObjectiveNode(ctx context.Context, id string, plan bool) (WorldNodeView, error) {
	objective, err := s.worldObjective(ctx, id, plan)
	if err != nil {
		return WorldNodeView{}, err
	}
	var records []domain.KR
	if err := s.db.WithContext(ctx).Where("objective_id = ?", objective.ID).Order("sort_order, id").Find(&records).Error; err != nil {
		return WorldNodeView{}, fmt.Errorf("list objective KRs: %w", err)
	}
	owners, err := s.objectiveOwners(ctx, objective.ID)
	if err != nil {
		return WorldNodeView{}, err
	}
	view := ObjectiveView{ID: objective.ID, Title: objective.Title, Version: objective.Version, Owners: owners, KRs: make([]KRView, 0, len(records))}
	for _, record := range records {
		kr, err := s.loadKRDefinitionWithGuard(ctx, record, plan, false)
		if err != nil {
			return WorldNodeView{}, err
		}
		view.KRs = append(view.KRs, kr)
	}
	data := map[string]any{"quarter": objective.Quarter, "objective": view}
	if plan {
		planRecord, err := s.worldPlan(ctx, objective.PlanID)
		if err != nil {
			return WorldNodeView{}, err
		}
		data["plan_id"] = planRecord.ID
		data["plan_title"] = planRecord.Title
	}
	return WorldNodeView{Name: objective.Title, Data: data}, nil
}

func (s *Service) resolveKRNode(ctx context.Context, id string, plan bool) (WorldNodeView, error) {
	var record domain.KR
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		return WorldNodeView{}, worldNodeError("get KR", err)
	}
	objective, err := s.worldObjective(ctx, record.ObjectiveID, plan)
	if err != nil {
		return WorldNodeView{}, err
	}
	view, err := s.loadKRDefinition(ctx, record, plan)
	if err != nil {
		return WorldNodeView{}, err
	}
	data := map[string]any{"quarter": objective.Quarter, "objective_id": objective.ID, "objective_title": objective.Title, "kr": view}
	if plan {
		planRecord, err := s.worldPlan(ctx, objective.PlanID)
		if err != nil {
			return WorldNodeView{}, err
		}
		data["plan_id"] = planRecord.ID
		data["plan_title"] = planRecord.Title
	}
	return WorldNodeView{Name: record.Title, Data: data}, nil
}

func (s *Service) resolvePointNode(ctx context.Context, id string, plan bool) (WorldNodeView, error) {
	var point domain.KRPoint
	if err := s.db.WithContext(ctx).First(&point, "id = ?", id).Error; err != nil {
		return WorldNodeView{}, worldNodeError("get point", err)
	}
	krNode, err := s.resolveKRNode(ctx, point.KRID, plan)
	if err != nil {
		return WorldNodeView{}, err
	}
	data, ok := krNode.Data.(map[string]any)
	if !ok {
		return WorldNodeView{}, fmt.Errorf("resolve point %s: invalid KR node payload", id)
	}
	kr, ok := data["kr"].(KRView)
	if !ok {
		return WorldNodeView{}, fmt.Errorf("resolve point %s: invalid KR definition", id)
	}
	for _, item := range kr.Points {
		if item.ID != id {
			continue
		}
		delete(data, "kr")
		data["kr_id"] = kr.ID
		data["kr_title"] = kr.Title
		data["point"] = item
		return WorldNodeView{Name: item.Title, Data: data}, nil
	}
	return WorldNodeView{}, ErrNotFound
}

func (s *Service) worldObjective(ctx context.Context, id string, plan bool) (domain.Objective, error) {
	query := "id = ? AND plan_id = ''"
	if plan {
		query = "id = ? AND plan_id <> ''"
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, query, id).Error; err != nil {
		return domain.Objective{}, worldNodeError("get objective", err)
	}
	return objective, nil
}

func (s *Service) worldPlan(ctx context.Context, id string) (domain.OKRPlan, error) {
	var plan domain.OKRPlan
	if err := s.db.WithContext(ctx).First(&plan, "id = ?", id).Error; err != nil {
		return domain.OKRPlan{}, worldNodeError("get OKR plan", err)
	}
	return plan, nil
}

func worldNodeError(operation string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", operation, err)
}
