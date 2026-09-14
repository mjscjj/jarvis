package okrworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

var regionalAlignmentRegions = map[string]struct{}{
	"eu": {}, "menat": {}, "sea-cca": {}, "nea": {}, "ams-anz": {},
}

var defaultRegionalCategoryOrder = []string{"公会业务", "运营效率", "优质主播 & 内容专项", "AI提效"}

type RegionalAlignmentView struct {
	ID           string `json:"id"`
	Quarter      string `json:"quarter"`
	PlanID       string `json:"plan_id"`
	RecapQuarter string `json:"recap_quarter"`
	Version      int32  `json:"version"`
}

type RegionalAlignmentRegionView struct {
	RegionCode    string   `json:"region_code"`
	Version       int32    `json:"version"`
	CategoryOrder []string `json:"category_order"`
}

type RegionalDemandView struct {
	ID           string                 `json:"id"`
	Version      int32                  `json:"version"`
	RegionalOKR  string                 `json:"regional_okr"`
	Item         string                 `json:"item"`
	Requirement  string                 `json:"requirement"`
	Docs         []domain.DocLink       `json:"docs"`
	Images       []domain.ImageRef      `json:"images"`
	Priority     string                 `json:"priority"`
	RegionalPOCs []domain.FollowUpOwner `json:"regional_pocs"`
	PlatformPOCs []domain.FollowUpOwner `json:"platform_pocs"`
	Acceptance   string                 `json:"acceptance"`
	PlanKRIDs    []string               `json:"plan_kr_ids"`
	Deliverable  string                 `json:"deliverable"`
	SortOrder    int                    `json:"sort_order"`
}

type RegionalPlanDecisionView struct {
	PlanKRID      string                 `json:"plan_kr_id"`
	Version       int32                  `json:"version"`
	Onboard       string                 `json:"onboard"`
	LaunchRegions []string               `json:"launch_regions"`
	RegionalPOCs  []domain.FollowUpOwner `json:"regional_pocs"`
	RegionalOKR   string                 `json:"regional_okr"`
	Hidden        bool                   `json:"hidden"`
}

type RegionalRecapOverlayView struct {
	BucketKey   string `json:"bucket_key"`
	ObjectiveID string `json:"objective_id"`
	Version     int32  `json:"version"`
	SortOrder   int    `json:"sort_order"`
	Hidden      bool   `json:"hidden"`
}

type RegionalAlignmentBoard struct {
	Alignment RegionalAlignmentView       `json:"alignment"`
	Region    RegionalAlignmentRegionView `json:"region"`
	Plan      PlanView                    `json:"plan"`
	Recap     Board                       `json:"recap"`
	Demands   []RegionalDemandView        `json:"demands"`
	Decisions []RegionalPlanDecisionView  `json:"decisions"`
	Overlays  []RegionalRecapOverlayView  `json:"recap_overlays"`
}

type RegionalDemandInput struct {
	ExpectedVersion int32                  `json:"expected_version"`
	RegionalOKR     string                 `json:"regional_okr"`
	Item            string                 `json:"item"`
	Requirement     string                 `json:"requirement"`
	Docs            []domain.DocLink       `json:"docs"`
	Images          []domain.ImageRef      `json:"images"`
	Priority        string                 `json:"priority"`
	RegionalPOCs    []domain.FollowUpOwner `json:"regional_pocs"`
	PlatformPOCs    []domain.FollowUpOwner `json:"platform_pocs"`
	Acceptance      string                 `json:"acceptance"`
	PlanKRIDs       []string               `json:"plan_kr_ids"`
	Deliverable     string                 `json:"deliverable"`
	SortOrder       int                    `json:"sort_order"`
}

type RegionalPlanDecisionInput struct {
	ExpectedVersion int32                  `json:"expected_version"`
	Onboard         string                 `json:"onboard"`
	LaunchRegions   []string               `json:"launch_regions"`
	RegionalPOCs    []domain.FollowUpOwner `json:"regional_pocs"`
	RegionalOKR     string                 `json:"regional_okr"`
	Hidden          bool                   `json:"hidden"`
}

func normalizeRegionalScope(quarter, region string) (string, string, error) {
	quarter, region = strings.TrimSpace(quarter), strings.ToLower(strings.TrimSpace(region))
	if !quarterPattern.MatchString(quarter) {
		return "", "", fmt.Errorf("quarter must use YYYY-Qn")
	}
	if _, ok := regionalAlignmentRegions[region]; !ok {
		return "", "", fmt.Errorf("unsupported region %q", region)
	}
	return quarter, region, nil
}

func previousQuarterString(quarter string) string {
	var year, number int
	if _, err := fmt.Sscanf(quarter, "%d-Q%d", &year, &number); err != nil {
		return ""
	}
	if number == 1 {
		return fmt.Sprintf("%d-Q4", year-1)
	}
	return fmt.Sprintf("%d-Q%d", year, number-1)
}

func regionalAlignmentID(quarter string) string {
	return "regional-alignment-" + strings.ToLower(quarter)
}

func planKRIDs(plan PlanView) map[string]struct{} {
	result := make(map[string]struct{})
	for _, objective := range plan.Objectives {
		for _, kr := range objective.KRs {
			result[kr.ID] = struct{}{}
		}
	}
	return result
}

func (s *Service) validateRegionalPlanKRIDs(ctx context.Context, alignment domain.RegionalAlignment, ids []string) error {
	plan, err := s.GetPlan(ctx, alignment.PlanID)
	if err != nil {
		return err
	}
	allowed := planKRIDs(plan)
	for _, id := range ids {
		if _, ok := allowed[id]; !ok {
			return fmt.Errorf("plan KR %q does not belong to alignment plan", id)
		}
	}
	return nil
}

func (s *Service) validateRegionalRecapObjectives(ctx context.Context, alignment domain.RegionalAlignment, ids []string) error {
	board, err := s.BizCoreBoard(ctx, alignment.RecapQuarter)
	if err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(board.Objectives))
	for _, objective := range board.Objectives {
		allowed[objective.ID] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := allowed[id]; !ok {
			return fmt.Errorf("recap objective %q does not belong to %s", id, alignment.RecapQuarter)
		}
	}
	return nil
}

func (s *Service) ensureRegionalAlignment(ctx context.Context, quarter, region, actor string) (domain.RegionalAlignment, domain.RegionalAlignmentRegion, error) {
	quarter, region, err := normalizeRegionalScope(quarter, region)
	if err != nil {
		return domain.RegionalAlignment{}, domain.RegionalAlignmentRegion{}, err
	}
	var alignment domain.RegionalAlignment
	err = s.db.WithContext(ctx).Where("quarter = ?", quarter).First(&alignment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		plans, listErr := s.ListPlans(ctx, quarter)
		if listErr != nil {
			return alignment, domain.RegionalAlignmentRegion{}, listErr
		}
		if len(plans.Plans) == 0 {
			return alignment, domain.RegionalAlignmentRegion{}, fmt.Errorf("quarter %s has no Biz OKR Plan", quarter)
		}
		now := time.Now().UTC()
		alignment = domain.RegionalAlignment{ID: regionalAlignmentID(quarter), Quarter: quarter, PlanID: plans.Plans[0].ID, RecapQuarter: previousQuarterString(quarter), Version: 1, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
		if createErr := s.db.WithContext(ctx).Create(&alignment).Error; createErr != nil {
			if !strings.Contains(strings.ToLower(createErr.Error()), "unique") {
				return alignment, domain.RegionalAlignmentRegion{}, fmt.Errorf("create regional alignment: %w", createErr)
			}
			if readErr := s.db.WithContext(ctx).Where("quarter = ?", quarter).First(&alignment).Error; readErr != nil {
				return alignment, domain.RegionalAlignmentRegion{}, readErr
			}
		}
	} else if err != nil {
		return alignment, domain.RegionalAlignmentRegion{}, fmt.Errorf("get regional alignment: %w", err)
	}

	var settings domain.RegionalAlignmentRegion
	err = s.db.WithContext(ctx).Where("alignment_id = ? AND region_code = ?", alignment.ID, region).First(&settings).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		now := time.Now().UTC()
		settings = domain.RegionalAlignmentRegion{AlignmentID: alignment.ID, RegionCode: region, Version: 1, CategoryOrder: append([]string(nil), defaultRegionalCategoryOrder...), UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
		if createErr := s.db.WithContext(ctx).Create(&settings).Error; createErr != nil {
			return alignment, settings, fmt.Errorf("create regional settings: %w", createErr)
		}
	} else if err != nil {
		return alignment, settings, fmt.Errorf("get regional settings: %w", err)
	}
	return alignment, settings, nil
}

func (s *Service) RegionalAlignmentBoard(ctx context.Context, quarter, region, actor string) (RegionalAlignmentBoard, error) {
	alignment, settings, err := s.ensureRegionalAlignment(ctx, quarter, region, actor)
	if err != nil {
		return RegionalAlignmentBoard{}, err
	}
	plan, err := s.GetPlan(ctx, alignment.PlanID)
	if err != nil {
		return RegionalAlignmentBoard{}, err
	}
	recap, err := s.BizCoreBoard(ctx, alignment.RecapQuarter)
	if err != nil {
		return RegionalAlignmentBoard{}, err
	}
	var demandRows []domain.RegionalDemand
	if err := s.db.WithContext(ctx).Where("alignment_id = ? AND region_code = ?", alignment.ID, settings.RegionCode).Order("sort_order, created_at, id").Find(&demandRows).Error; err != nil {
		return RegionalAlignmentBoard{}, fmt.Errorf("list regional demands: %w", err)
	}
	var decisionRows []domain.RegionalPlanDecision
	if err := s.db.WithContext(ctx).Where("alignment_id = ? AND region_code = ?", alignment.ID, settings.RegionCode).Find(&decisionRows).Error; err != nil {
		return RegionalAlignmentBoard{}, fmt.Errorf("list regional decisions: %w", err)
	}
	var overlayRows []domain.RegionalRecapOverlay
	if err := s.db.WithContext(ctx).Where("alignment_id = ? AND region_code = ?", alignment.ID, settings.RegionCode).Order("bucket_key, sort_order, objective_id").Find(&overlayRows).Error; err != nil {
		return RegionalAlignmentBoard{}, fmt.Errorf("list regional recap overlays: %w", err)
	}
	result := RegionalAlignmentBoard{
		Alignment: regionalAlignmentView(alignment),
		Region:    RegionalAlignmentRegionView{RegionCode: settings.RegionCode, Version: settings.Version, CategoryOrder: nonNilStrings(settings.CategoryOrder)},
		Plan:      plan, Recap: recap,
		Demands: make([]RegionalDemandView, 0, len(demandRows)), Decisions: make([]RegionalPlanDecisionView, 0, len(decisionRows)), Overlays: make([]RegionalRecapOverlayView, 0, len(overlayRows)),
	}
	for _, row := range demandRows {
		result.Demands = append(result.Demands, regionalDemandView(row))
	}
	for _, row := range decisionRows {
		result.Decisions = append(result.Decisions, regionalDecisionView(row))
	}
	for _, row := range overlayRows {
		result.Overlays = append(result.Overlays, RegionalRecapOverlayView{BucketKey: row.BucketKey, ObjectiveID: row.ObjectiveID, Version: row.Version, SortOrder: row.SortOrder, Hidden: row.Hidden})
	}
	return result, nil
}

func regionalAlignmentView(row domain.RegionalAlignment) RegionalAlignmentView {
	return RegionalAlignmentView{ID: row.ID, Quarter: row.Quarter, PlanID: row.PlanID, RecapQuarter: row.RecapQuarter, Version: row.Version}
}

func regionalDemandView(row domain.RegionalDemand) RegionalDemandView {
	return RegionalDemandView{ID: row.ID, Version: row.Version, RegionalOKR: row.RegionalOKR, Item: row.Item, Requirement: row.Requirement, Docs: nonNilDocs(row.Docs), Images: nonNilImages(row.Images), Priority: row.Priority, RegionalPOCs: append([]domain.FollowUpOwner(nil), row.RegionalPOCs...), PlatformPOCs: append([]domain.FollowUpOwner(nil), row.PlatformPOCs...), Acceptance: row.Acceptance, PlanKRIDs: nonNilStrings(row.PlanKRIDs), Deliverable: row.Deliverable, SortOrder: row.SortOrder}
}

func regionalDecisionView(row domain.RegionalPlanDecision) RegionalPlanDecisionView {
	return RegionalPlanDecisionView{PlanKRID: row.PlanKRID, Version: row.Version, Onboard: row.Onboard, LaunchRegions: nonNilStrings(row.LaunchRegions), RegionalPOCs: append([]domain.FollowUpOwner(nil), row.RegionalPOCs...), RegionalOKR: row.RegionalOKR, Hidden: row.Hidden}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
func normalizeRegionalDemandInput(input RegionalDemandInput) (RegionalDemandInput, error) {
	input.RegionalOKR, input.Item, input.Requirement, input.Deliverable = strings.TrimSpace(input.RegionalOKR), strings.TrimSpace(input.Item), strings.TrimSpace(input.Requirement), strings.TrimSpace(input.Deliverable)
	input.Priority, input.Acceptance = strings.ToLower(strings.TrimSpace(input.Priority)), strings.ToLower(strings.TrimSpace(input.Acceptance))
	if input.Priority != "" && input.Priority != "p0" && input.Priority != "p1" && input.Priority != "p2" {
		return input, fmt.Errorf("priority must be p0, p1 or p2")
	}
	if input.Acceptance == "" {
		input.Acceptance = "tbd"
	}
	if input.Acceptance != "yes" && input.Acceptance != "no" && input.Acceptance != "tbd" {
		return input, fmt.Errorf("acceptance must be yes, no or tbd")
	}
	input.PlanKRIDs = uniqueCleanStrings(input.PlanKRIDs)
	return input, nil
}

func uniqueCleanStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			if _, ok := seen[clean]; !ok {
				seen[clean] = struct{}{}
				result = append(result, clean)
			}
		}
	}
	return result
}

func regionalJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode regional alignment value: %w", err)
	}
	return string(encoded), nil
}

func (s *Service) CreateRegionalDemand(ctx context.Context, quarter, region, actor string, input RegionalDemandInput) (RegionalDemandView, error) {
	alignment, _, err := s.ensureRegionalAlignment(ctx, quarter, region, actor)
	if err != nil {
		return RegionalDemandView{}, err
	}
	if err := s.verifyPeople(ctx, input.RegionalPOCs); err != nil {
		return RegionalDemandView{}, err
	}
	if err := s.verifyPeople(ctx, input.PlatformPOCs); err != nil {
		return RegionalDemandView{}, err
	}
	input, err = normalizeRegionalDemandInput(input)
	if err != nil {
		return RegionalDemandView{}, err
	}
	if input.ExpectedVersion != 0 {
		return RegionalDemandView{}, fmt.Errorf("expected_version must be zero when creating")
	}
	if err := s.validateRegionalPlanKRIDs(ctx, alignment, input.PlanKRIDs); err != nil {
		return RegionalDemandView{}, err
	}
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", alignment.ID, region, now.UnixNano())))
	row := domain.RegionalDemand{ID: fmt.Sprintf("regional-demand-%x", digest[:10]), AlignmentID: alignment.ID, RegionCode: strings.ToLower(strings.TrimSpace(region)), Version: 1, RegionalOKR: input.RegionalOKR, Item: input.Item, Requirement: input.Requirement, Docs: input.Docs, Images: input.Images, Priority: input.Priority, RegionalPOCs: input.RegionalPOCs, PlatformPOCs: input.PlatformPOCs, Acceptance: input.Acceptance, PlanKRIDs: input.PlanKRIDs, Deliverable: input.Deliverable, SortOrder: input.SortOrder, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return RegionalDemandView{}, fmt.Errorf("create regional demand: %w", err)
	}
	return regionalDemandView(row), nil
}

func (s *Service) UpdateRegionalDemand(ctx context.Context, quarter, region, id, actor string, input RegionalDemandInput) (RegionalDemandView, error) {
	alignment, _, err := s.ensureRegionalAlignment(ctx, quarter, region, actor)
	if err != nil {
		return RegionalDemandView{}, err
	}
	if err := s.verifyPeople(ctx, input.RegionalPOCs); err != nil {
		return RegionalDemandView{}, err
	}
	if err := s.verifyPeople(ctx, input.PlatformPOCs); err != nil {
		return RegionalDemandView{}, err
	}
	input, err = normalizeRegionalDemandInput(input)
	if err != nil {
		return RegionalDemandView{}, err
	}
	if err := s.validateRegionalPlanKRIDs(ctx, alignment, input.PlanKRIDs); err != nil {
		return RegionalDemandView{}, err
	}
	var row domain.RegionalDemand
	if err := s.db.WithContext(ctx).Where("id = ? AND alignment_id = ? AND region_code = ?", strings.TrimSpace(id), alignment.ID, strings.ToLower(strings.TrimSpace(region))).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return RegionalDemandView{}, ErrNotFound
	} else if err != nil {
		return RegionalDemandView{}, err
	}
	if input.ExpectedVersion != row.Version {
		return RegionalDemandView{}, ErrConflict
	}
	docsJSON, err := regionalJSON(input.Docs)
	if err != nil {
		return RegionalDemandView{}, err
	}
	imagesJSON, err := regionalJSON(input.Images)
	if err != nil {
		return RegionalDemandView{}, err
	}
	regionalPOCsJSON, err := regionalJSON(input.RegionalPOCs)
	if err != nil {
		return RegionalDemandView{}, err
	}
	platformPOCsJSON, err := regionalJSON(input.PlatformPOCs)
	if err != nil {
		return RegionalDemandView{}, err
	}
	planKRIDsJSON, err := regionalJSON(input.PlanKRIDs)
	if err != nil {
		return RegionalDemandView{}, err
	}
	now := time.Now().UTC()
	updates := map[string]any{"regional_okr": input.RegionalOKR, "item": input.Item, "requirement": input.Requirement, "docs": docsJSON, "images": imagesJSON, "priority": input.Priority, "regional_po_cs": regionalPOCsJSON, "platform_po_cs": platformPOCsJSON, "acceptance": input.Acceptance, "plan_kr_ids": planKRIDsJSON, "deliverable": input.Deliverable, "sort_order": input.SortOrder, "updated_by": actor, "updated_at": now, "version": gorm.Expr("version + 1")}
	result := s.db.WithContext(ctx).Model(&domain.RegionalDemand{}).Where("id = ? AND version = ?", row.ID, row.Version).Updates(updates)
	if result.Error != nil {
		return RegionalDemandView{}, result.Error
	}
	if result.RowsAffected != 1 {
		return RegionalDemandView{}, ErrConflict
	}
	row.Version++
	row.RegionalOKR = input.RegionalOKR
	row.Item = input.Item
	row.Requirement = input.Requirement
	row.Docs = input.Docs
	row.Images = input.Images
	row.Priority = input.Priority
	row.RegionalPOCs = input.RegionalPOCs
	row.PlatformPOCs = input.PlatformPOCs
	row.Acceptance = input.Acceptance
	row.PlanKRIDs = input.PlanKRIDs
	row.Deliverable = input.Deliverable
	row.SortOrder = input.SortOrder
	return regionalDemandView(row), nil
}

func (s *Service) DeleteRegionalDemand(ctx context.Context, quarter, region, id string, expectedVersion int32) error {
	alignment, _, err := s.ensureRegionalAlignment(ctx, quarter, region, "")
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("id = ? AND alignment_id = ? AND region_code = ? AND version = ?", strings.TrimSpace(id), alignment.ID, strings.ToLower(strings.TrimSpace(region)), expectedVersion).Delete(&domain.RegionalDemand{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Service) PutRegionalDecision(ctx context.Context, quarter, region, krID, actor string, input RegionalPlanDecisionInput) (RegionalPlanDecisionView, error) {
	if err := s.verifyPeople(ctx, input.RegionalPOCs); err != nil {
		return RegionalPlanDecisionView{}, err
	}
	alignment, _, err := s.ensureRegionalAlignment(ctx, quarter, region, actor)
	if err != nil {
		return RegionalPlanDecisionView{}, err
	}
	input.Onboard = strings.ToLower(strings.TrimSpace(input.Onboard))
	if input.Onboard != "" && input.Onboard != "yes" && input.Onboard != "no" {
		return RegionalPlanDecisionView{}, fmt.Errorf("onboard must be yes or no")
	}
	region = strings.ToLower(strings.TrimSpace(region))
	krID = strings.TrimSpace(krID)
	if krID == "" {
		return RegionalPlanDecisionView{}, fmt.Errorf("plan KR id is required")
	}
	if err := s.validateRegionalPlanKRIDs(ctx, alignment, []string{krID}); err != nil {
		return RegionalPlanDecisionView{}, err
	}
	var row domain.RegionalPlanDecision
	err = s.db.WithContext(ctx).Where("alignment_id = ? AND region_code = ? AND plan_kr_id = ?", alignment.ID, region, krID).First(&row).Error
	now := time.Now().UTC()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if input.ExpectedVersion != 0 {
			return RegionalPlanDecisionView{}, ErrConflict
		}
		row = domain.RegionalPlanDecision{AlignmentID: alignment.ID, RegionCode: region, PlanKRID: krID, Version: 1, Onboard: input.Onboard, LaunchRegions: uniqueCleanStrings(input.LaunchRegions), RegionalPOCs: input.RegionalPOCs, RegionalOKR: strings.TrimSpace(input.RegionalOKR), Hidden: input.Hidden, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return RegionalPlanDecisionView{}, err
		}
		return regionalDecisionView(row), nil
	} else if err != nil {
		return RegionalPlanDecisionView{}, err
	}
	if row.Version != input.ExpectedVersion {
		return RegionalPlanDecisionView{}, ErrConflict
	}
	launchRegions := uniqueCleanStrings(input.LaunchRegions)
	launchRegionsJSON, err := regionalJSON(launchRegions)
	if err != nil {
		return RegionalPlanDecisionView{}, err
	}
	regionalPOCsJSON, err := regionalJSON(input.RegionalPOCs)
	if err != nil {
		return RegionalPlanDecisionView{}, err
	}
	updates := map[string]any{"onboard": input.Onboard, "launch_regions": launchRegionsJSON, "regional_po_cs": regionalPOCsJSON, "regional_okr": strings.TrimSpace(input.RegionalOKR), "hidden": input.Hidden, "updated_by": actor, "updated_at": now, "version": gorm.Expr("version + 1")}
	result := s.db.WithContext(ctx).Model(&domain.RegionalPlanDecision{}).Where("alignment_id = ? AND region_code = ? AND plan_kr_id = ? AND version = ?", alignment.ID, region, krID, row.Version).Updates(updates)
	if result.Error != nil {
		return RegionalPlanDecisionView{}, result.Error
	}
	if result.RowsAffected != 1 {
		return RegionalPlanDecisionView{}, ErrConflict
	}
	row.Version++
	row.Onboard = input.Onboard
	row.LaunchRegions = launchRegions
	row.RegionalPOCs = input.RegionalPOCs
	row.RegionalOKR = strings.TrimSpace(input.RegionalOKR)
	row.Hidden = input.Hidden
	return regionalDecisionView(row), nil
}

type RegionalSettingsInput struct {
	ExpectedVersion int32    `json:"expected_version"`
	CategoryOrder   []string `json:"category_order"`
}

func (s *Service) PutRegionalSettings(ctx context.Context, quarter, region, actor string, input RegionalSettingsInput) (RegionalAlignmentRegionView, error) {
	alignment, row, err := s.ensureRegionalAlignment(ctx, quarter, region, actor)
	if err != nil {
		return RegionalAlignmentRegionView{}, err
	}
	_ = alignment
	if row.Version != input.ExpectedVersion {
		return RegionalAlignmentRegionView{}, ErrConflict
	}
	order := uniqueCleanStrings(input.CategoryOrder)
	if len(order) == 0 {
		return RegionalAlignmentRegionView{}, fmt.Errorf("category_order is required")
	}
	orderJSON, err := regionalJSON(order)
	if err != nil {
		return RegionalAlignmentRegionView{}, err
	}
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.RegionalAlignmentRegion{}).Where("alignment_id = ? AND region_code = ? AND version = ?", row.AlignmentID, row.RegionCode, row.Version).Updates(map[string]any{"category_order": orderJSON, "updated_by": actor, "updated_at": now, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return RegionalAlignmentRegionView{}, result.Error
	}
	if result.RowsAffected != 1 {
		return RegionalAlignmentRegionView{}, ErrConflict
	}
	return RegionalAlignmentRegionView{RegionCode: row.RegionCode, Version: row.Version + 1, CategoryOrder: order}, nil
}

type RegionalRecapOrderInput struct {
	BucketKey    string   `json:"bucket_key"`
	ObjectiveIDs []string `json:"objective_ids"`
}

func (s *Service) PutRegionalRecapOrder(ctx context.Context, quarter, region, actor string, input RegionalRecapOrderInput) ([]RegionalRecapOverlayView, error) {
	alignment, _, err := s.ensureRegionalAlignment(ctx, quarter, region, actor)
	if err != nil {
		return nil, err
	}
	input.BucketKey = strings.TrimSpace(input.BucketKey)
	ids := uniqueCleanStrings(input.ObjectiveIDs)
	if input.BucketKey == "" {
		return nil, fmt.Errorf("bucket_key is required")
	}
	if err := s.validateRegionalRecapObjectives(ctx, alignment, ids); err != nil {
		return nil, err
	}
	region = strings.ToLower(strings.TrimSpace(region))
	now := time.Now().UTC()
	for index, id := range ids {
		var row domain.RegionalRecapOverlay
		err := s.db.WithContext(ctx).Where("alignment_id = ? AND region_code = ? AND bucket_key = ? AND objective_id = ?", alignment.ID, region, input.BucketKey, id).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = domain.RegionalRecapOverlay{AlignmentID: alignment.ID, RegionCode: region, BucketKey: input.BucketKey, ObjectiveID: id, Version: 1, SortOrder: index, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
			if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		} else {
			if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{"sort_order": index, "updated_by": actor, "updated_at": now, "version": gorm.Expr("version + 1")}).Error; err != nil {
				return nil, err
			}
		}
	}
	var rows []domain.RegionalRecapOverlay
	if err := s.db.WithContext(ctx).Where("alignment_id = ? AND region_code = ? AND bucket_key = ?", alignment.ID, region, input.BucketKey).Order("sort_order, objective_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]RegionalRecapOverlayView, 0, len(rows))
	for _, row := range rows {
		result = append(result, RegionalRecapOverlayView{BucketKey: row.BucketKey, ObjectiveID: row.ObjectiveID, Version: row.Version, SortOrder: row.SortOrder, Hidden: row.Hidden})
	}
	return result, nil
}

type RegionalRecapPatchInput struct {
	ExpectedVersion int32 `json:"expected_version"`
	Hidden          bool  `json:"hidden"`
}

func (s *Service) PatchRegionalRecap(ctx context.Context, quarter, region, bucket, objectiveID, actor string, input RegionalRecapPatchInput) (RegionalRecapOverlayView, error) {
	alignment, _, err := s.ensureRegionalAlignment(ctx, quarter, region, actor)
	if err != nil {
		return RegionalRecapOverlayView{}, err
	}
	region = strings.ToLower(strings.TrimSpace(region))
	bucket = strings.TrimSpace(bucket)
	objectiveID = strings.TrimSpace(objectiveID)
	if bucket == "" || objectiveID == "" {
		return RegionalRecapOverlayView{}, fmt.Errorf("bucket and objective id are required")
	}
	if err := s.validateRegionalRecapObjectives(ctx, alignment, []string{objectiveID}); err != nil {
		return RegionalRecapOverlayView{}, err
	}
	now := time.Now().UTC()
	var row domain.RegionalRecapOverlay
	err = s.db.WithContext(ctx).Where("alignment_id = ? AND region_code = ? AND bucket_key = ? AND objective_id = ?", alignment.ID, region, bucket, objectiveID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if input.ExpectedVersion != 0 {
			return RegionalRecapOverlayView{}, ErrConflict
		}
		row = domain.RegionalRecapOverlay{AlignmentID: alignment.ID, RegionCode: region, BucketKey: bucket, ObjectiveID: objectiveID, Version: 1, Hidden: input.Hidden, SortOrder: 0, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return RegionalRecapOverlayView{}, err
		}
	} else if err != nil {
		return RegionalRecapOverlayView{}, err
	} else {
		if row.Version != input.ExpectedVersion {
			return RegionalRecapOverlayView{}, ErrConflict
		}
		result := s.db.WithContext(ctx).Model(&row).Where("version = ?", row.Version).Updates(map[string]any{"hidden": input.Hidden, "updated_by": actor, "updated_at": now, "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return RegionalRecapOverlayView{}, result.Error
		}
		if result.RowsAffected != 1 {
			return RegionalRecapOverlayView{}, ErrConflict
		}
		row.Version++
		row.Hidden = input.Hidden
	}
	return RegionalRecapOverlayView{BucketKey: row.BucketKey, ObjectiveID: row.ObjectiveID, Version: row.Version, SortOrder: row.SortOrder, Hidden: row.Hidden}, nil
}
