package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/gorm"
)

type ProjectRiskList struct {
	Items    []ProjectRiskView `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

type ProjectRiskFilter struct {
	ListFilter
	ProjectID     uint64
	IncludeClosed bool
}

type ProjectRiskService struct {
	db     *gorm.DB
	events *progress.Service
	now    func() time.Time
}

func NewProjectRiskService(db *gorm.DB) (*ProjectRiskService, error) {
	if db == nil {
		return nil, fmt.Errorf("project risk service db is nil")
	}
	events, err := progress.NewService(db)
	if err != nil {
		return nil, err
	}
	return &ProjectRiskService{db: db, events: events, now: time.Now}, nil
}

func (s *ProjectRiskService) Create(ctx context.Context, in ProjectRiskInput) (*ProjectRiskView, error) {
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if in.TriggeredAt != nil {
		return nil, invalid(fmt.Errorf("new project risk must start untriggered"))
	}
	if err := requireProjectID(ctx, s.db, in.ProjectID); err != nil {
		return nil, err
	}
	item := domain.ProjectRisk{
		ProjectID: in.ProjectID, Title: in.Title, Probability: in.Probability, Impact: in.Impact,
		TriggeredAt: in.TriggeredAt,
	}
	if err := s.db.WithContext(ctx).Create(&item).Error; err != nil {
		return nil, fmt.Errorf("create project risk: %w", err)
	}
	occurredAt := item.CreatedAt
	if occurredAt.IsZero() {
		occurredAt = s.now().UTC()
	}
	if _, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: PageTypeProjectRisk, SubjectID: item.ID,
		Description: fmt.Sprintf("登记项目风险“%s”。", item.Title),
		OccurredAt:  &occurredAt, SourceKind: &factSourceBackground,
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, item.ID)
}

func (s *ProjectRiskService) Get(ctx context.Context, id uint64) (*ProjectRiskView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("project risk id must be positive"))
	}
	var item domain.ProjectRisk
	err := s.db.WithContext(ctx).Preload("Project").Where("id = ?", id).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project risk id=%d: %w", id, err)
	}
	view := toProjectRiskView(&item)
	return &view, nil
}

func (s *ProjectRiskService) Update(ctx context.Context, id uint64, in ProjectRiskInput) (*ProjectRiskView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("project risk id must be positive"))
	}
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := requireProjectID(ctx, s.db, in.ProjectID); err != nil {
		return nil, err
	}
	var before domain.ProjectRisk
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&before).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load project risk id=%d: %w", id, err)
	}
	if before.ClosedAt != nil {
		return nil, invalid(fmt.Errorf("project risk id=%d is closed", id))
	}
	if before.TriggeredAt != nil && (in.TriggeredAt == nil || !before.TriggeredAt.Equal(*in.TriggeredAt)) {
		return nil, invalid(fmt.Errorf("project risk id=%d is already triggered", id))
	}
	if before.ProjectID == in.ProjectID && before.Title == in.Title && before.Probability == in.Probability &&
		before.Impact == in.Impact && sameOptionalTime(before.TriggeredAt, in.TriggeredAt) {
		return s.Get(ctx, id)
	}
	updates := map[string]any{
		"project_id": in.ProjectID, "title": in.Title, "probability": in.Probability,
		"impact": in.Impact, "triggered_at": in.TriggeredAt,
	}
	query := s.db.WithContext(ctx).Model(&domain.ProjectRisk{}).Where("id = ? AND closed_at IS NULL", id)
	if before.TriggeredAt == nil {
		query = query.Where("triggered_at IS NULL")
	} else {
		query = query.Where("triggered_at = ?", *before.TriggeredAt)
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update project risk id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, invalid(fmt.Errorf("project risk id=%d changed concurrently", id))
	}
	if before.TriggeredAt == nil && in.TriggeredAt != nil {
		if err := s.appendFact(ctx, id, *in.TriggeredAt, fmt.Sprintf("项目风险“%s”已触发。", in.Title)); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, id)
}

func (s *ProjectRiskService) Delete(ctx context.Context, id uint64) error {
	item, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.ClosedAt != nil {
		return invalid(fmt.Errorf("project risk id=%d is already closed", id))
	}
	now := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.ProjectRisk{}).Where("id = ? AND closed_at IS NULL", id).Update("closed_at", now)
	if result.Error != nil {
		return fmt.Errorf("close project risk id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("close project risk id=%d affected %d rows", id, result.RowsAffected)
	}
	return s.appendFact(ctx, id, now, fmt.Sprintf("项目风险“%s”已关闭。", item.Title))
}

func (s *ProjectRiskService) List(ctx context.Context, filter ProjectRiskFilter) (*ProjectRiskList, error) {
	if err := filter.validate(); err != nil {
		return nil, invalid(err)
	}
	query := s.db.WithContext(ctx).Model(&domain.ProjectRisk{})
	if filter.ProjectID != 0 {
		query = query.Where("project_id = ?", filter.ProjectID)
	}
	if !filter.IncludeClosed {
		query = query.Where("closed_at IS NULL")
	}
	if filter.Keyword != "" {
		like := keywordPattern(filter.Keyword)
		query = query.Where(`title LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\' OR probability LIKE ? ESCAPE '\' OR impact LIKE ? ESCAPE '\'`, like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count project risks: %w", err)
	}
	items := make([]domain.ProjectRisk, 0, filter.PageSize)
	if total > 0 {
		if err := query.Preload("Project").Order("triggered_at IS NULL ASC, triggered_at DESC, id DESC").Limit(filter.PageSize).Offset(filter.offset()).Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list project risks: %w", err)
		}
	}
	return &ProjectRiskList{Items: toProjectRiskViews(items), Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *ProjectRiskService) appendFact(ctx context.Context, id uint64, at time.Time, description string) error {
	_, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: PageTypeProjectRisk, SubjectID: id, Description: description,
		OccurredAt: &at, SourceKind: &factSourceBackground,
	})
	return err
}

func requireProjectID(ctx context.Context, db *gorm.DB, projectID uint64) error {
	var count int64
	if err := db.WithContext(ctx).Model(&domain.Project{}).Where("id = ?", projectID).Count(&count).Error; err != nil {
		return fmt.Errorf("validate project id=%d: %w", projectID, err)
	}
	if count != 1 {
		return invalid(fmt.Errorf("project id=%d does not exist", projectID))
	}
	return nil
}
