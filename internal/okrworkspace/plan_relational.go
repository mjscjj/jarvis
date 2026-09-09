package okrworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

func (s *Service) planObjectives(ctx context.Context, planID string) ([]PlanObjectiveView, error) {
	var objectives []domain.Objective
	if err := s.db.WithContext(ctx).Where("plan_id = ?", planID).Order("sort_order, id").Find(&objectives).Error; err != nil {
		return nil, fmt.Errorf("list OKR plan objectives: %w", err)
	}
	result := make([]PlanObjectiveView, 0, len(objectives))
	for _, objective := range objectives {
		var krs []domain.KR
		if err := s.db.WithContext(ctx).Where("objective_id = ?", objective.ID).Order("sort_order, id").Find(&krs).Error; err != nil {
			return nil, fmt.Errorf("list plan KRs for %s: %w", objective.ID, err)
		}
		view := PlanObjectiveView{ID: objective.ID, Title: objective.Title, Version: objective.Version, KRs: make([]PlanKRView, 0, len(krs))}
		for _, kr := range krs {
			definition, err := s.loadKRDefinition(ctx, kr, true)
			if err != nil {
				return nil, fmt.Errorf("load plan KR %s: %w", kr.ID, err)
			}
			view.KRs = append(view.KRs, planKRFromDefinition(definition))
		}
		result = append(result, view)
	}
	return normalizePlanObjectives(result)
}

func planKRFromDefinition(value KRView) PlanKRView {
	points := make([]PlanPointView, 0, len(value.Points))
	for _, point := range value.Points {
		points = append(points, PlanPointView{ID: point.ID, Version: point.Version, Kind: point.Kind, Title: point.Title, MeegoWorkItemID: point.MeegoWorkItemID, MeegoURL: point.MeegoURL, Owners: append([]OwnerView(nil), point.Owners...), Tags: append([]TagView(nil), point.Tags...)})
	}
	return PlanKRView{
		ID: value.ID, Title: value.Title, Version: value.Version, Owners: append([]OwnerView(nil), value.Owners...),
		MetricNote: value.MetricNote, Metrics: append([]MetricView(nil), value.Metrics...), Points: points, Tags: append([]TagView(nil), value.Tags...),
	}
}

func (s *Service) writePlanObjectiveChildren(ctx context.Context, objective PlanObjectiveView, actor string, now time.Time) error {
	db := s.db.WithContext(ctx)
	for krIndex, kr := range objective.KRs {
		row := domain.KR{ID: kr.ID, ObjectiveID: objective.ID, Title: kr.Title, MetricNote: kr.MetricNote, SortOrder: krIndex, Version: kr.Version, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&row).Error; err != nil {
			return fmt.Errorf("create plan KR %s: %w", kr.ID, err)
		}
		if err := replaceKROwners(db, kr.ID, normalizeOwners(kr.Owners)); err != nil {
			return err
		}
		for metricIndex, metric := range kr.Metrics {
			metric.ID = strings.TrimSpace(metric.ID)
			if err := db.Create(&domain.KRMetric{ID: metric.ID, KRID: kr.ID, Text: metric.Text, Light: metric.Light, Images: nonNilImages(metric.Images), SortOrder: metricIndex}).Error; err != nil {
				return fmt.Errorf("create plan metric %s: %w", metric.ID, err)
			}
		}
		for pointIndex, point := range kr.Points {
			pointRow := domain.KRPoint{ID: point.ID, KRID: kr.ID, Version: point.Version, Kind: point.Kind, Title: point.Title, MeegoWorkItemID: strings.TrimSpace(point.MeegoWorkItemID), MeegoURL: strings.TrimSpace(point.MeegoURL), SortOrder: pointIndex}
			if err := db.Create(&pointRow).Error; err != nil {
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
		for _, tag := range kr.Tags {
			if err := db.Create(&domain.KRTag{KRID: kr.ID, Type: tag.Type, Value: tag.Value}).Error; err != nil {
				return fmt.Errorf("create plan KR tag: %w", err)
			}
		}
	}
	return nil
}

// updatePlanObjectiveChildren updates the KR-owned rows of an existing Plan
// objective without deleting and recreating its points. Existing point rows,
// wording and owners are exclusively owned by the point endpoint, so an old
// browser snapshot cannot overwrite a collaborator's point edit.
func (s *Service) updatePlanObjectiveChildren(ctx context.Context, objective PlanObjectiveView, actor string, now time.Time) error {
	db := s.db.WithContext(ctx)
	var existingKRs []domain.KR
	if err := db.Where("objective_id = ?", objective.ID).Find(&existingKRs).Error; err != nil {
		return fmt.Errorf("list existing plan KRs: %w", err)
	}
	existingKRByID := make(map[string]domain.KR, len(existingKRs))
	for _, kr := range existingKRs {
		existingKRByID[kr.ID] = kr
	}
	incomingKRIDs := make(map[string]struct{}, len(objective.KRs))
	for krIndex, kr := range objective.KRs {
		incomingKRIDs[kr.ID] = struct{}{}
		if current, exists := existingKRByID[kr.ID]; exists {
			updates := map[string]any{"title": kr.Title, "metric_note": kr.MetricNote, "sort_order": krIndex, "version": gorm.Expr("version + 1"), "updated_at": now, "updated_by": actor}
			result := db.Model(&domain.KR{}).Where("id = ? AND objective_id = ? AND version = ?", kr.ID, objective.ID, current.Version).Updates(updates)
			if result.Error != nil {
				return fmt.Errorf("update plan KR %s: %w", kr.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrConflict
			}
		} else {
			row := domain.KR{ID: kr.ID, ObjectiveID: objective.ID, Title: kr.Title, MetricNote: kr.MetricNote, SortOrder: krIndex, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
			if err := db.Create(&row).Error; err != nil {
				return fmt.Errorf("create plan KR %s: %w", kr.ID, err)
			}
		}
		if err := replaceKROwners(db, kr.ID, normalizeOwners(kr.Owners)); err != nil {
			return err
		}
		if err := db.Where("kr_id = ?", kr.ID).Delete(&domain.KRMetric{}).Error; err != nil {
			return fmt.Errorf("replace plan KR metrics: %w", err)
		}
		for metricIndex, metric := range kr.Metrics {
			if err := db.Create(&domain.KRMetric{ID: metric.ID, KRID: kr.ID, Text: metric.Text, Light: metric.Light, Images: nonNilImages(metric.Images), SortOrder: metricIndex}).Error; err != nil {
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
		existingPointByID := make(map[string]domain.KRPoint, len(existingPoints))
		for _, point := range existingPoints {
			existingPointByID[point.ID] = point
		}
		incomingPointIDs := make(map[string]struct{}, len(kr.Points))
		for pointIndex, point := range kr.Points {
			incomingPointIDs[point.ID] = struct{}{}
			if _, exists := existingPointByID[point.ID]; exists {
				// Only ordering is structural. All editable point definition fields
				// remain untouched and are saved through PatchPointDefinition.
				if err := db.Model(&domain.KRPoint{}).Where("id = ? AND kr_id = ?", point.ID, kr.ID).Update("sort_order", pointIndex).Error; err != nil {
					return fmt.Errorf("update plan point order: %w", err)
				}
				continue
			}
			pointRow := domain.KRPoint{ID: point.ID, KRID: kr.ID, Version: point.Version, Kind: point.Kind, Title: point.Title, MeegoWorkItemID: strings.TrimSpace(point.MeegoWorkItemID), MeegoURL: strings.TrimSpace(point.MeegoURL), SortOrder: pointIndex}
			if err := db.Create(&pointRow).Error; err != nil {
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
		if err := purgePoints(db, removedPointIDs, true); err != nil {
			return err
		}
	}
	for _, kr := range existingKRs {
		if _, kept := incomingKRIDs[kr.ID]; kept {
			continue
		}
		var pointIDs []string
		if err := db.Model(&domain.KRPoint{}).Where("kr_id = ?", kr.ID).Pluck("id", &pointIDs).Error; err != nil {
			return fmt.Errorf("list removed plan KR points: %w", err)
		}
		if err := purgePoints(db, pointIDs, true); err != nil {
			return err
		}
		for _, model := range []any{&domain.KROwner{}, &domain.KRTag{}, &domain.KRMetric{}} {
			if err := db.Where("kr_id = ?", kr.ID).Delete(model).Error; err != nil {
				return fmt.Errorf("delete removed plan KR children: %w", err)
			}
		}
		if err := db.Delete(&domain.KR{}, "id = ?", kr.ID).Error; err != nil {
			return fmt.Errorf("delete removed plan KR: %w", err)
		}
	}
	return nil
}

func (s *Service) deletePlanObjectiveChildren(ctx context.Context, objectiveID string) error {
	db := s.db.WithContext(ctx)
	var krIDs []string
	if err := db.Model(&domain.KR{}).Where("objective_id = ?", objectiveID).Pluck("id", &krIDs).Error; err != nil {
		return fmt.Errorf("list plan objective KRs for delete: %w", err)
	}
	if len(krIDs) == 0 {
		return nil
	}
	var pointIDs []string
	if err := db.Model(&domain.KRPoint{}).Where("kr_id IN ?", krIDs).Pluck("id", &pointIDs).Error; err != nil {
		return fmt.Errorf("list plan objective points for delete: %w", err)
	}
	if len(pointIDs) > 0 {
		for _, model := range []any{&domain.PointOwner{}, &domain.PointTag{}} {
			if err := db.Where("point_id IN ?", pointIDs).Delete(model).Error; err != nil {
				return fmt.Errorf("delete plan objective point children: %w", err)
			}
		}
	}
	for _, model := range []any{&domain.KROwner{}, &domain.KRTag{}, &domain.KRMetric{}, &domain.KRPoint{}} {
		if err := db.Where("kr_id IN ?", krIDs).Delete(model).Error; err != nil {
			return fmt.Errorf("delete plan objective KR children: %w", err)
		}
	}
	if err := db.Where("id IN ?", krIDs).Delete(&domain.KR{}).Error; err != nil {
		return fmt.Errorf("delete plan objective KRs: %w", err)
	}
	return nil
}

func (s *Service) deletePlanDefinitionRows(ctx context.Context, planID string) error {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return fmt.Errorf("plan id is required")
	}
	db := s.db.WithContext(ctx)
	var objectiveIDs []string
	if err := db.Model(&domain.Objective{}).Where("plan_id = ?", planID).Pluck("id", &objectiveIDs).Error; err != nil {
		return fmt.Errorf("list plan objectives for delete: %w", err)
	}
	if len(objectiveIDs) == 0 {
		return nil
	}
	var krIDs []string
	if err := db.Model(&domain.KR{}).Where("objective_id IN ?", objectiveIDs).Pluck("id", &krIDs).Error; err != nil {
		return fmt.Errorf("list plan KRs for delete: %w", err)
	}
	if len(krIDs) > 0 {
		var pointIDs []string
		if err := db.Model(&domain.KRPoint{}).Where("kr_id IN ?", krIDs).Pluck("id", &pointIDs).Error; err != nil {
			return fmt.Errorf("list plan points for delete: %w", err)
		}
		if len(pointIDs) > 0 {
			for _, model := range []any{&domain.PointOwner{}, &domain.PointTag{}} {
				if err := db.Where("point_id IN ?", pointIDs).Delete(model).Error; err != nil {
					return fmt.Errorf("delete plan point children: %w", err)
				}
			}
		}
		for _, model := range []any{&domain.KROwner{}, &domain.KRTag{}, &domain.KRMetric{}, &domain.KRPoint{}} {
			if err := db.Where("kr_id IN ?", krIDs).Delete(model).Error; err != nil {
				return fmt.Errorf("delete plan KR children: %w", err)
			}
		}
		if err := db.Where("id IN ?", krIDs).Delete(&domain.KR{}).Error; err != nil {
			return fmt.Errorf("delete plan KRs: %w", err)
		}
	}
	if err := db.Where("plan_id = ?", planID).Delete(&domain.Objective{}).Error; err != nil {
		return fmt.Errorf("delete plan objectives: %w", err)
	}
	return nil
}

func (s *Service) planObjective(ctx context.Context, planID, objectiveID string) (domain.Objective, error) {
	var objective domain.Objective
	err := s.db.WithContext(ctx).Where("id = ? AND plan_id = ?", strings.TrimSpace(objectiveID), strings.TrimSpace(planID)).First(&objective).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Objective{}, ErrNotFound
	}
	if err != nil {
		return domain.Objective{}, fmt.Errorf("get plan objective: %w", err)
	}
	return objective, nil
}
