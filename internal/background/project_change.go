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

type ProjectChangeList struct {
	Items    []ProjectChangeView `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

type ProjectChangeFilter struct {
	ListFilter
	ProjectID     uint64
	IncludeClosed bool
}

type ProjectChangeService struct {
	db     *gorm.DB
	events *progress.Service
	now    func() time.Time
}

func NewProjectChangeService(db *gorm.DB) (*ProjectChangeService, error) {
	if db == nil {
		return nil, fmt.Errorf("project change service db is nil")
	}
	events, err := progress.NewService(db)
	if err != nil {
		return nil, err
	}
	return &ProjectChangeService{db: db, events: events, now: time.Now}, nil
}

func (s *ProjectChangeService) Create(ctx context.Context, in ProjectChangeInput) (*ProjectChangeView, error) {
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := requireProjectID(ctx, s.db, in.ProjectID); err != nil {
		return nil, err
	}
	item := domain.ProjectChange{
		ProjectID: in.ProjectID, Title: in.Title, ChangedAt: in.ChangedAt.UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&item).Error; err != nil {
		return nil, fmt.Errorf("create project change: %w", err)
	}
	if _, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: PageTypeProjectChange, SubjectID: item.ID,
		Description: fmt.Sprintf("记录项目变更“%s”。", item.Title),
		OccurredAt:  &item.ChangedAt, SourceKind: &factSourceBackground,
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, item.ID)
}

func (s *ProjectChangeService) Get(ctx context.Context, id uint64) (*ProjectChangeView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("project change id must be positive"))
	}
	var item domain.ProjectChange
	err := s.db.WithContext(ctx).Preload("Project").Where("id = ?", id).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project change id=%d: %w", id, err)
	}
	view := toProjectChangeView(&item)
	return &view, nil
}

func (s *ProjectChangeService) Update(ctx context.Context, id uint64, in ProjectChangeInput) (*ProjectChangeView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("project change id must be positive"))
	}
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := requireProjectID(ctx, s.db, in.ProjectID); err != nil {
		return nil, err
	}
	var before domain.ProjectChange
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&before).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load project change id=%d: %w", id, err)
	}
	if before.ClosedAt != nil {
		return nil, invalid(fmt.Errorf("project change id=%d is closed", id))
	}
	if before.ProjectID == in.ProjectID && before.Title == in.Title && before.ChangedAt.Equal(in.ChangedAt) {
		return s.Get(ctx, id)
	}
	updates := map[string]any{
		"project_id": in.ProjectID, "title": in.Title,
		"changed_at": in.ChangedAt.UTC(),
	}
	result := s.db.WithContext(ctx).Model(&domain.ProjectChange{}).Where("id = ? AND closed_at IS NULL", id).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update project change id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, invalid(fmt.Errorf("project change id=%d changed concurrently", id))
	}
	return s.Get(ctx, id)
}

func (s *ProjectChangeService) Delete(ctx context.Context, id uint64) error {
	item, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if item.ClosedAt != nil {
		return invalid(fmt.Errorf("project change id=%d is already closed", id))
	}
	now := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.ProjectChange{}).Where("id = ? AND closed_at IS NULL", id).Update("closed_at", now)
	if result.Error != nil {
		return fmt.Errorf("close project change id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("close project change id=%d affected %d rows", id, result.RowsAffected)
	}
	return s.appendFact(ctx, id, now, fmt.Sprintf("项目变更“%s”已关闭。", item.Title))
}

func (s *ProjectChangeService) List(ctx context.Context, filter ProjectChangeFilter) (*ProjectChangeList, error) {
	if err := filter.validate(); err != nil {
		return nil, invalid(err)
	}
	query := s.db.WithContext(ctx).Model(&domain.ProjectChange{})
	if filter.ProjectID != 0 {
		query = query.Where("project_id = ?", filter.ProjectID)
	}
	if !filter.IncludeClosed {
		query = query.Where("closed_at IS NULL")
	}
	if filter.Keyword != "" {
		like := keywordPattern(filter.Keyword)
		query = query.Where(`title LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\'`, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count project changes: %w", err)
	}
	items := make([]domain.ProjectChange, 0, filter.PageSize)
	if total > 0 {
		if err := query.Preload("Project").Order("changed_at DESC, id DESC").Limit(filter.PageSize).Offset(filter.offset()).Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list project changes: %w", err)
		}
	}
	return &ProjectChangeList{Items: toProjectChangeViews(items), Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *ProjectChangeService) appendFact(ctx context.Context, id uint64, at time.Time, description string) error {
	_, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: PageTypeProjectChange, SubjectID: id, Description: description,
		OccurredAt: &at, SourceKind: &factSourceBackground,
	})
	return err
}
