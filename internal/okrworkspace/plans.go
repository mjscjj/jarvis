package okrworkspace

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

type PlanView struct {
	ID          string              `json:"id"`
	Quarter     string              `json:"quarter"`
	Title       string              `json:"title"`
	Version     int32               `json:"version"`
	DeleteToken string              `json:"delete_token"`
	Objectives  []PlanObjectiveView `json:"objectives"`
	CreatedBy   string              `json:"created_by"`
	UpdatedBy   string              `json:"updated_by"`
	CreatedAt   string              `json:"created_at"`
	UpdatedAt   string              `json:"updated_at"`
}

type PlanSummaryView struct {
	ID           string `json:"id"`
	Quarter      string `json:"quarter"`
	Title        string `json:"title"`
	Version      int32  `json:"version"`
	ObjectiveCnt int    `json:"objective_count"`
	KRCnt        int    `json:"kr_count"`
	UpdatedAt    string `json:"updated_at"`
}

type PlanListView struct {
	Quarter           string            `json:"quarter"`
	AvailableQuarters []string          `json:"available_quarters"`
	Plans             []PlanSummaryView `json:"plans"`
}

type PlanObjectiveView struct {
	ID             string       `json:"id"`
	Title          string       `json:"title"`
	Version        int32        `json:"version"`
	StructureToken string       `json:"structure_token"`
	KRs            []PlanKRView `json:"krs"`
}

type PlanKRView struct {
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Version    int32           `json:"version"`
	Owners     []OwnerView     `json:"owners"`
	MetricNote string          `json:"metric_note"`
	Metrics    []MetricView    `json:"metrics"`
	Points     []PlanPointView `json:"points"`
	Tags       []TagView       `json:"tags"`
}

type PlanPointView struct {
	ID              string           `json:"id"`
	Version         int32            `json:"version"`
	Kind            domain.PointKind `json:"kind"`
	Title           string           `json:"title"`
	MeegoWorkItemID string           `json:"meego_work_item_id"`
	MeegoURL        string           `json:"meego_url"`
	Owners          []OwnerView      `json:"owners"`
	Tags            []TagView        `json:"tags"`
}

type PlanObjectiveNodeView struct {
	PlanID    string            `json:"plan_id"`
	PlanTitle string            `json:"plan_title"`
	Quarter   string            `json:"quarter"`
	Objective PlanObjectiveView `json:"objective"`
}

type PlanKRNodeView struct {
	PlanID         string     `json:"plan_id"`
	PlanTitle      string     `json:"plan_title"`
	Quarter        string     `json:"quarter"`
	ObjectiveID    string     `json:"objective_id"`
	ObjectiveTitle string     `json:"objective_title"`
	KR             PlanKRView `json:"kr"`
}

type PlanPointNodeView struct {
	PlanID         string        `json:"plan_id"`
	PlanTitle      string        `json:"plan_title"`
	Quarter        string        `json:"quarter"`
	ObjectiveID    string        `json:"objective_id"`
	ObjectiveTitle string        `json:"objective_title"`
	KRID           string        `json:"kr_id"`
	KRTitle        string        `json:"kr_title"`
	Point          PlanPointView `json:"point"`
}

type CreatePlanInput struct {
	Quarter   string `json:"quarter"`
	Title     string `json:"title"`
	CreatedBy string `json:"-"`
}

func (s *Service) ListPlans(ctx context.Context, quarter string) (PlanListView, error) {
	quarter = strings.TrimSpace(quarter)
	if quarter == "" {
		quarter = latestQuarterString()
	}
	if !quarterPattern.MatchString(quarter) {
		return PlanListView{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	quarters, err := s.ListPlanQuarters(ctx)
	if err != nil {
		return PlanListView{}, err
	}
	if !containsString(quarters, quarter) {
		quarters = append([]string{quarter}, quarters...)
	}
	var records []domain.OKRPlan
	if err := s.db.WithContext(ctx).Where("quarter = ?", quarter).Order("updated_at DESC, id DESC").Find(&records).Error; err != nil {
		return PlanListView{}, fmt.Errorf("list OKR plans: %w", err)
	}
	result := PlanListView{Quarter: quarter, AvailableQuarters: quarters, Plans: make([]PlanSummaryView, 0, len(records))}
	for _, record := range records {
		view, err := s.planFromRecord(ctx, record)
		if err != nil {
			return PlanListView{}, err
		}
		result.Plans = append(result.Plans, PlanSummaryView{
			ID: record.ID, Quarter: record.Quarter, Title: record.Title, Version: record.Version,
			ObjectiveCnt: len(view.Objectives), KRCnt: countPlanKRs(view.Objectives),
			UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return result, nil
}

func (s *Service) ListPlanQuarters(ctx context.Context) ([]string, error) {
	var quarters []string
	if err := s.db.WithContext(ctx).Model(&domain.OKRPlan{}).Distinct().Where("quarter <> ''").Order("quarter DESC").Pluck("quarter", &quarters).Error; err != nil {
		return nil, fmt.Errorf("list OKR plan quarters: %w", err)
	}
	for _, quarter := range quarters {
		if !quarterPattern.MatchString(quarter) {
			return nil, fmt.Errorf("invalid OKR plan quarter in storage: %q", quarter)
		}
	}
	coreQuarters, err := s.ListQuarters(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(quarters)+len(coreQuarters))
	for _, quarter := range append(quarters, coreQuarters...) {
		if _, ok := seen[quarter]; ok {
			continue
		}
		seen[quarter] = struct{}{}
		result = append(result, quarter)
	}
	if len(result) == 0 {
		result = append(result, latestQuarterString())
	}
	return result, nil
}

func (s *Service) GetPlan(ctx context.Context, id string) (PlanView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return PlanView{}, fmt.Errorf("plan id is required")
	}
	var record domain.OKRPlan
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PlanView{}, ErrNotFound
		}
		return PlanView{}, fmt.Errorf("get OKR plan: %w", err)
	}
	return s.planFromRecord(ctx, record)
}

func (s *Service) GetPlanObjectiveNode(ctx context.Context, id string) (PlanObjectiveNodeView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return PlanObjectiveNodeView{}, fmt.Errorf("plan objective id is required")
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ? AND plan_id <> ''", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PlanObjectiveNodeView{}, ErrNotFound
		}
		return PlanObjectiveNodeView{}, fmt.Errorf("get plan objective: %w", err)
	}
	plan, err := s.GetPlan(ctx, objective.PlanID)
	if err != nil {
		return PlanObjectiveNodeView{}, err
	}
	for _, item := range plan.Objectives {
		if item.ID == id {
			return PlanObjectiveNodeView{PlanID: plan.ID, PlanTitle: plan.Title, Quarter: plan.Quarter, Objective: item}, nil
		}
	}
	return PlanObjectiveNodeView{}, ErrNotFound
}

func (s *Service) GetPlanKRNode(ctx context.Context, id string) (PlanKRNodeView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return PlanKRNodeView{}, fmt.Errorf("plan KR id is required")
	}
	var kr domain.KR
	if err := s.db.WithContext(ctx).First(&kr, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PlanKRNodeView{}, ErrNotFound
		}
		return PlanKRNodeView{}, fmt.Errorf("get plan KR: %w", err)
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ? AND plan_id <> ''", kr.ObjectiveID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PlanKRNodeView{}, ErrNotFound
		}
		return PlanKRNodeView{}, fmt.Errorf("get plan objective for KR: %w", err)
	}
	plan, err := s.GetPlan(ctx, objective.PlanID)
	if err != nil {
		return PlanKRNodeView{}, err
	}
	for _, objectiveView := range plan.Objectives {
		if objectiveView.ID != objective.ID {
			continue
		}
		for _, item := range objectiveView.KRs {
			if item.ID == id {
				return PlanKRNodeView{
					PlanID: plan.ID, PlanTitle: plan.Title, Quarter: plan.Quarter,
					ObjectiveID: objective.ID, ObjectiveTitle: objective.Title, KR: item,
				}, nil
			}
		}
	}
	return PlanKRNodeView{}, ErrNotFound
}

func (s *Service) GetPlanPointNode(ctx context.Context, id string) (PlanPointNodeView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return PlanPointNodeView{}, fmt.Errorf("plan point id is required")
	}
	var point domain.KRPoint
	if err := s.db.WithContext(ctx).First(&point, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PlanPointNodeView{}, ErrNotFound
		}
		return PlanPointNodeView{}, fmt.Errorf("get plan point: %w", err)
	}
	kr, err := s.GetPlanKRNode(ctx, point.KRID)
	if err != nil {
		return PlanPointNodeView{}, err
	}
	for _, item := range kr.KR.Points {
		if item.ID == id {
			return PlanPointNodeView{
				PlanID: kr.PlanID, PlanTitle: kr.PlanTitle, Quarter: kr.Quarter,
				ObjectiveID: kr.ObjectiveID, ObjectiveTitle: kr.ObjectiveTitle,
				KRID: kr.KR.ID, KRTitle: kr.KR.Title, Point: item,
			}, nil
		}
	}
	return PlanPointNodeView{}, ErrNotFound
}

func (s *Service) CreatePlan(ctx context.Context, input CreatePlanInput) (PlanView, error) {
	input.Quarter = strings.TrimSpace(input.Quarter)
	input.Title = strings.TrimSpace(input.Title)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if !quarterPattern.MatchString(input.Quarter) {
		return PlanView{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	if input.Title == "" {
		return PlanView{}, fmt.Errorf("plan title is required")
	}
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", input.Quarter, input.Title, now.UnixNano())))
	record := domain.OKRPlan{
		ID: fmt.Sprintf("plan-%x", digest[:10]), Quarter: input.Quarter, Title: input.Title,
		CreatedBy: input.CreatedBy, UpdatedBy: input.CreatedBy,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return PlanView{}, fmt.Errorf("create OKR plan: %w", err)
	}
	return s.planFromRecord(ctx, record)
}

func (s *Service) DeletePlan(ctx context.Context, id, expectedToken string) error {
	id = strings.TrimSpace(id)
	expectedToken = strings.TrimSpace(expectedToken)
	if id == "" || expectedToken == "" {
		return fmt.Errorf("plan id and delete_token are required")
	}
	current, err := s.GetPlan(ctx, id)
	if err != nil {
		return err
	}
	if current.DeleteToken != expectedToken {
		return ErrConflict
	}
	if err := s.deletePlanDefinitionRows(ctx, id); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Where("plan_id = ?", id).Delete(&domain.PageComment{}).Error; err != nil {
		return fmt.Errorf("delete OKR plan comments: %w", err)
	}
	result := s.db.WithContext(ctx).Delete(&domain.OKRPlan{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("delete OKR plan: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) planFromRecord(ctx context.Context, record domain.OKRPlan) (PlanView, error) {
	objectives, err := s.planObjectives(ctx, record.ID)
	if err != nil {
		return PlanView{}, err
	}
	objectives = withPlanOwnerIdentityNamespaces(objectives)
	view := PlanView{
		ID: record.ID, Quarter: record.Quarter, Title: record.Title, Version: record.Version, Objectives: objectives,
		CreatedBy: record.CreatedBy, UpdatedBy: record.UpdatedBy,
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
	}
	deleteToken, err := s.planDeletionToken(ctx, view)
	if err != nil {
		return PlanView{}, err
	}
	view.DeleteToken = deleteToken
	return view, nil
}

func withPlanOwnerIdentityNamespaces(objectives []PlanObjectiveView) []PlanObjectiveView {
	for objectiveIndex := range objectives {
		for krIndex := range objectives[objectiveIndex].KRs {
			kr := &objectives[objectiveIndex].KRs[krIndex]
			for ownerIndex, owner := range kr.Owners {
				kr.Owners[ownerIndex] = storedOwnerView(owner.OpenID, owner.Name)
			}
			for pointIndex := range kr.Points {
				for ownerIndex, owner := range kr.Points[pointIndex].Owners {
					kr.Points[pointIndex].Owners[ownerIndex] = storedOwnerView(owner.OpenID, owner.Name)
				}
			}
		}
	}
	return objectives
}

func normalizePlanObjectives(input []PlanObjectiveView) ([]PlanObjectiveView, error) {
	seen := map[string]bool{}
	for objectiveIndex := range input {
		objective := &input[objectiveIndex]
		objective.ID = strings.TrimSpace(objective.ID)
		objective.Title = strings.TrimSpace(objective.Title)
		if objective.ID == "" || seen[objective.ID] {
			return nil, fmt.Errorf("plan objectives require unique ids")
		}
		seen[objective.ID] = true
		for krIndex := range objective.KRs {
			kr := &objective.KRs[krIndex]
			kr.ID = strings.TrimSpace(kr.ID)
			kr.Title = strings.TrimSpace(kr.Title)
			kr.MetricNote = strings.TrimSpace(kr.MetricNote)
			if kr.ID == "" || seen[kr.ID] {
				return nil, fmt.Errorf("plan KRs require unique ids")
			}
			seen[kr.ID] = true
			kr.Owners = normalizeOwners(kr.Owners)
			kr.Tags = normalizeTags(kr.Tags)
			if err := validateTags(kr.Tags); err != nil {
				return nil, err
			}
			for metricIndex := range kr.Metrics {
				metric := &kr.Metrics[metricIndex]
				metric.ID = strings.TrimSpace(metric.ID)
				metric.Text = strings.TrimSpace(metric.Text)
				if metric.ID == "" || seen[metric.ID] || !domain.ValidLight(metric.Light) {
					return nil, fmt.Errorf("plan metrics require unique ids and a valid light")
				}
				seen[metric.ID] = true
				metric.Images = nonNilImages(metric.Images)
			}
			for pointIndex := range kr.Points {
				point := &kr.Points[pointIndex]
				point.ID = strings.TrimSpace(point.ID)
				point.Title = strings.TrimSpace(point.Title)
				if point.ID == "" || seen[point.ID] || !domain.ValidPointKind(point.Kind) {
					return nil, fmt.Errorf("plan points require unique ids and a valid kind")
				}
				seen[point.ID] = true
				point.MeegoWorkItemID = strings.TrimSpace(point.MeegoWorkItemID)
				point.MeegoURL = strings.TrimSpace(point.MeegoURL)
				point.Owners = normalizeOwners(point.Owners)
				point.Tags = normalizeTags(point.Tags)
				if err := validatePointTags(point.Tags); err != nil {
					return nil, fmt.Errorf("invalid plan point tags for %s: %w", point.ID, err)
				}
			}
		}
	}
	if input == nil {
		input = []PlanObjectiveView{}
	}
	return input, nil
}

func countPlanKRs(objectives []PlanObjectiveView) int {
	total := 0
	for _, objective := range objectives {
		total += len(objective.KRs)
	}
	return total
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func latestQuarterString() string {
	now := time.Now().UTC()
	return fmt.Sprintf("%d-Q%d", now.Year(), (int(now.Month())-1)/3+1)
}
