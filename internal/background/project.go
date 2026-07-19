package background

import (
	"context"
	"errors"
	"fmt"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ErrNotFound is returned when a background row does not exist. Callers map it
// to HTTP 404; every other error is a genuine failure and must surface.
var ErrNotFound = errors.New("background record not found")

// ErrInvalidInput wraps every caller-input validation failure so the API layer
// can map it to HTTP 400 while genuine storage failures surface as 500.
var ErrInvalidInput = errors.New("invalid background input")

// invalid wraps a validation failure with the ErrInvalidInput sentinel.
func invalid(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrInvalidInput, err)
}

// ProjectList is the paginated response for projects.
type ProjectList struct {
	Items    []ProjectView `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

// ProjectService is the authoritative CRUD owner of the project table.
type ProjectService struct {
	db *gorm.DB
}

func NewProjectService(db *gorm.DB) (*ProjectService, error) {
	if db == nil {
		return nil, fmt.Errorf("project service db is nil")
	}
	return &ProjectService{db: db}, nil
}

func (s *ProjectService) Create(ctx context.Context, in ProjectInput) (*ProjectView, error) {
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	project := domain.Project{
		Code:         in.Code,
		Name:         in.Name,
		Role:         in.Role,
		Status:       in.Status,
		Priority:     in.Priority,
		Description:  in.Description,
		Repos:        datatypes.JSON(in.Repos),
		TechStack:    datatypes.JSON(in.TechStack),
		KeyDecisions: datatypes.JSON(in.KeyDecisions),
		Timeline:     datatypes.JSON(in.Timeline),
		Notes:        in.Notes,
	}
	if err := s.db.WithContext(ctx).Create(&project).Error; err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	view := toProjectView(&project)
	return &view, nil
}

func (s *ProjectService) Get(ctx context.Context, id uint64) (*ProjectView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("project id must be positive"))
	}
	var project domain.Project
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project id=%d: %w", id, err)
	}
	view := toProjectView(&project)
	return &view, nil
}

// GetByCode looks a project up by its unique code. Used by jarvis-tools so codex
// can resolve a project_hint (code) to full project detail.
func (s *ProjectService) GetByCode(ctx context.Context, code string) (*ProjectView, error) {
	if code == "" {
		return nil, invalid(fmt.Errorf("project code must not be empty"))
	}
	var project domain.Project
	err := s.db.WithContext(ctx).Where("code = ?", code).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get project code=%s: %w", code, err)
	}
	view := toProjectView(&project)
	return &view, nil
}

func (s *ProjectService) Update(ctx context.Context, id uint64, in ProjectInput) (*ProjectView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("project id must be positive"))
	}
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	// Explicit column list so an update never silently touches audit columns and
	// always overwrites JSON fields to NULL when the caller omits them.
	updates := map[string]any{
		"code":          in.Code,
		"name":          in.Name,
		"role":          in.Role,
		"status":        in.Status,
		"priority":      in.Priority,
		"description":   in.Description,
		"repos":         datatypes.JSON(in.Repos),
		"tech_stack":    datatypes.JSON(in.TechStack),
		"key_decisions": datatypes.JSON(in.KeyDecisions),
		"timeline":      datatypes.JSON(in.Timeline),
		"notes":         in.Notes,
	}
	result := s.db.WithContext(ctx).Model(&domain.Project{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update project id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *ProjectService) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return invalid(fmt.Errorf("project id must be positive"))
	}
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.Project{})
	if result.Error != nil {
		return fmt.Errorf("delete project id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAll returns every project (no pagination), ordered by priority. It is
// used by the jarvis-tools CLI so codex can scan the full project catalog when
// attributing a Todo to a project.
func (s *ProjectService) ListAll(ctx context.Context) ([]ProjectView, error) {
	items := make([]domain.Project, 0)
	if err := s.db.WithContext(ctx).Order("priority ASC, id DESC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list all projects: %w", err)
	}
	return toProjectViews(items), nil
}

func (s *ProjectService) List(ctx context.Context, filter ListFilter) (*ProjectList, error) {
	if err := filter.validate(); err != nil {
		return nil, invalid(err)
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&domain.Project{}).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count projects: %w", err)
	}
	items := make([]domain.Project, 0, filter.PageSize)
	if total > 0 {
		if err := s.db.WithContext(ctx).
			Order("priority ASC, id DESC").
			Limit(filter.PageSize).
			Offset(filter.offset()).
			Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
	}
	return &ProjectList{Items: toProjectViews(items), Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}
