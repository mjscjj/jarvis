package okrworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PatchPointDefinitionInput changes only fields owned by one concrete KR row.
// Pointers distinguish an omitted field from an explicit empty owners list.
type PatchPointDefinitionInput struct {
	ExpectedVersion int32             `json:"expected_version"`
	Title           *string           `json:"title"`
	Owners          *[]OwnerView      `json:"owners"`
	Kind            *domain.PointKind `json:"kind"`
	MeegoWorkItemID *string           `json:"meego_work_item_id"`
	MeegoURL        *string           `json:"meego_url"`
	Tags            *[]TagView        `json:"tags"`
	UpdatedBy       string            `json:"-"`
}

// PointDefinitionPatchResult is the canonical state of the single point that
// was written. Parent versions are deliberately absent: a point owns its own
// concurrency boundary and never invalidates a sibling point or its KR.
type PointDefinitionPatchResult struct {
	PointID          string           `json:"point_id"`
	Version          int32            `json:"version"`
	StructureToken   string           `json:"structure_token,omitempty"`
	KRStructureToken string           `json:"kr_structure_token,omitempty"`
	DeleteToken      string           `json:"delete_token,omitempty"`
	PlanDeleteToken  string           `json:"plan_delete_token,omitempty"`
	Title            string           `json:"title"`
	Kind             domain.PointKind `json:"kind"`
	MeegoWorkItemID  string           `json:"meego_work_item_id"`
	MeegoURL         string           `json:"meego_url"`
	Owners           []OwnerView      `json:"owners"`
	Tags             []TagView        `json:"tags"`
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
	if input.Title == nil && input.Owners == nil && input.Kind == nil && input.MeegoWorkItemID == nil && input.MeegoURL == nil && input.Tags == nil {
		return PointDefinitionPatchResult{}, fmt.Errorf("at least one point field is required")
	}
	if input.ExpectedVersion < 0 {
		return PointDefinitionPatchResult{}, fmt.Errorf("expected_version must be non-negative")
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
		if err := s.verifyPeople(ctx, normalized); err != nil {
			return PointDefinitionPatchResult{}, err
		}
		input.Owners = &normalized
	}
	if input.Kind != nil && !domain.ValidPointKind(*input.Kind) {
		return PointDefinitionPatchResult{}, fmt.Errorf("point kind is invalid")
	}
	if input.MeegoWorkItemID != nil {
		clean := strings.TrimSpace(*input.MeegoWorkItemID)
		input.MeegoWorkItemID = &clean
	}
	if input.MeegoURL != nil {
		clean := strings.TrimSpace(*input.MeegoURL)
		input.MeegoURL = &clean
	}
	if input.Tags != nil {
		normalized := normalizeTags(*input.Tags)
		if err := validatePointTags(normalized); err != nil {
			return PointDefinitionPatchResult{}, err
		}
		input.Tags = &normalized
	}

	db := s.db.WithContext(ctx)
	if input.Tags != nil && !db.Migrator().HasTable(&domain.PointTag{}) {
		return PointDefinitionPatchResult{}, fmt.Errorf("point tags are unavailable")
	}
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

	updates := map[string]any{"version": gorm.Expr("version + 1")}
	if input.Title != nil {
		updates["title"] = *input.Title
	}
	if input.Kind != nil {
		updates["kind"] = *input.Kind
	}
	if input.MeegoWorkItemID != nil {
		updates["meego_work_item_id"] = *input.MeegoWorkItemID
	}
	if input.MeegoURL != nil {
		updates["meego_url"] = *input.MeegoURL
	}
	bumped := domain.KRPoint{}
	result := db.Model(&bumped).Clauses(clause.Returning{Columns: []clause.Column{{Name: "version"}, {Name: "title"}}}).
		Where("id = ? AND kr_id = ? AND version = ?", point.ID, kr.ID, input.ExpectedVersion).Updates(updates)
	if result.Error != nil {
		return PointDefinitionPatchResult{}, fmt.Errorf("update point definition: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		current, currentErr := s.pointDefinitionResult(ctx, point.ID)
		if currentErr != nil {
			return PointDefinitionPatchResult{}, currentErr
		}
		return current, ErrConflict
	}
	if input.Owners != nil {
		if err := replacePointOwners(db, point.ID, *input.Owners); err != nil {
			return PointDefinitionPatchResult{}, err
		}
	}
	if input.Tags != nil {
		if err := db.Where("point_id = ?", point.ID).Delete(&domain.PointTag{}).Error; err != nil {
			return PointDefinitionPatchResult{}, fmt.Errorf("replace point tags: %w", err)
		}
		for _, tag := range *input.Tags {
			if err := db.Create(&domain.PointTag{PointID: point.ID, Type: tag.Type, Value: tag.Value}).Error; err != nil {
				return PointDefinitionPatchResult{}, fmt.Errorf("create point tag: %w", err)
			}
		}
	}
	if planID != "" && input.MeegoWorkItemID != nil && point.MeegoWorkItemID != *input.MeegoWorkItemID {
		if err := db.Where("point_id = ?", point.ID).Delete(&domain.MeegoSyncSnapshot{}).Error; err != nil {
			return PointDefinitionPatchResult{}, fmt.Errorf("reset changed Meego snapshot: %w", err)
		}
	}
	return s.pointDefinitionResult(ctx, point.ID)
}

func (s *Service) pointDefinitionResult(ctx context.Context, pointID string) (PointDefinitionPatchResult, error) {
	var point domain.KRPoint
	if err := s.db.WithContext(ctx).First(&point, "id = ?", pointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PointDefinitionPatchResult{}, ErrNotFound
		}
		return PointDefinitionPatchResult{}, fmt.Errorf("get point definition result: %w", err)
	}
	var records []domain.PointOwner
	if err := s.db.WithContext(ctx).Where("point_id = ?", pointID).Order("sort_order, owner_key, person_id").Find(&records).Error; err != nil {
		return PointDefinitionPatchResult{}, fmt.Errorf("list point definition owners: %w", err)
	}
	owners := make([]OwnerView, 0, len(records))
	for _, owner := range records {
		owners = append(owners, storedOwnerView(owner.Email, owner.Name, owner.UnionID))
	}
	var tagRecords []domain.PointTag
	if s.db.Migrator().HasTable(&domain.PointTag{}) {
		if err := s.db.WithContext(ctx).Where("point_id = ?", point.ID).Order("type, value").Find(&tagRecords).Error; err != nil {
			return PointDefinitionPatchResult{}, fmt.Errorf("list point definition tags: %w", err)
		}
	}
	tags := make([]TagView, 0, len(tagRecords))
	for _, tag := range tagRecords {
		tags = append(tags, TagView{Type: tag.Type, Value: tag.Value})
	}
	result := PointDefinitionPatchResult{
		PointID: point.ID, Version: point.Version, Title: point.Title, Kind: point.Kind,
		MeegoWorkItemID: point.MeegoWorkItemID, MeegoURL: point.MeegoURL, Owners: owners, Tags: tags,
	}
	var kr domain.KR
	if err := s.db.WithContext(ctx).First(&kr, "id = ?", point.KRID).Error; err != nil {
		return PointDefinitionPatchResult{}, fmt.Errorf("get point KR result: %w", err)
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ?", kr.ObjectiveID).Error; err != nil {
		return PointDefinitionPatchResult{}, fmt.Errorf("get point objective result: %w", err)
	}
	if objective.PlanID == "" {
		deleteToken, err := s.krDeletionToken(ctx, kr.ID)
		if err != nil {
			return PointDefinitionPatchResult{}, err
		}
		result.DeleteToken = deleteToken
	} else {
		plan, err := s.GetPlan(ctx, objective.PlanID)
		if err != nil {
			return PointDefinitionPatchResult{}, err
		}
		result.PlanDeleteToken = plan.DeleteToken
		for _, item := range plan.Objectives {
			if item.ID == objective.ID {
				result.StructureToken = item.StructureToken
				for _, krItem := range item.KRs {
					if krItem.ID == kr.ID {
						result.KRStructureToken = krItem.StructureToken
						break
					}
				}
				break
			}
		}
	}
	return result, nil
}
