package background

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/gorm"
	"jarvis/internal/datatypes"
)

// ErrNotFound is returned when a background row does not exist. Callers map it
// to HTTP 404; every other error is a genuine failure and must surface.
var ErrNotFound = errors.New("background record not found")

// ErrInvalidInput wraps every caller-input validation failure so the API layer
// can map it to HTTP 400 while genuine storage failures surface as 500.
var ErrInvalidInput = errors.New("invalid background input")

// ErrConflict wraps a unique-key violation so the API layer maps it to HTTP 409
// instead of a generic 500. Re-creating an existing record (for example a person
// keyed by open_id) is a caller conflict, not a server failure, and must not spam
// the error log.
var ErrConflict = errors.New("background record already exists")

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

// factSourceBackground tags facts this service writes from project CRUD.
var factSourceBackground = "background"

// ProjectService is the authoritative CRUD owner of the project table.
type ProjectService struct {
	db     *gorm.DB
	events *progress.Service
}

func NewProjectService(db *gorm.DB) (*ProjectService, error) {
	if db == nil {
		return nil, fmt.Errorf("project service db is nil")
	}
	events, err := progress.NewService(db)
	if err != nil {
		return nil, err
	}
	return &ProjectService{db: db, events: events}, nil
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
	occurredAt := project.CreatedAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if _, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: "project",
		SubjectID:   project.ID,
		Description: fmt.Sprintf("创建项目“%s”，当前状态为“%s”。", project.Name, project.Status),
		OccurredAt:  &occurredAt,
		SourceKind:  &factSourceBackground,
	}); err != nil {
		return nil, err
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
	err := s.db.WithContext(ctx).Where("code = ? AND status <> ?", code, "archived").Take(&project).Error
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
	var before domain.Project
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&before).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load project id=%d before update: %w", id, err)
	}
	changedFields := projectChangedFields(&before, in)
	if len(changedFields) == 0 {
		view := toProjectView(&before)
		return &view, nil
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
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("update project id=%d affected %d rows", id, result.RowsAffected)
	}
	now := time.Now().UTC()
	if before.Status != in.Status {
		if _, err := s.events.AppendFact(ctx, progress.FactInput{
			SubjectType: "project",
			SubjectID:   id,
			Description: fmt.Sprintf("项目状态从“%s”调整为“%s”。", before.Status, in.Status),
			OccurredAt:  &now,
			SourceKind:  &factSourceBackground,
		}); err != nil {
			return nil, err
		}
	}
	profileFields := withoutField(changedFields, "status")
	if len(profileFields) > 0 {
		if _, err := s.events.AppendFact(ctx, progress.FactInput{
			SubjectType: "project",
			SubjectID:   id,
			Description: fmt.Sprintf("更新项目资料：%s。", strings.Join(profileFields, "、")),
			OccurredAt:  &now,
			SourceKind:  &factSourceBackground,
		}); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, id)
}

func (s *ProjectService) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return invalid(fmt.Errorf("project id must be positive"))
	}
	var project domain.Project
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("load project id=%d before archive: %w", id, err)
	}
	if project.Status == "archived" {
		return invalid(fmt.Errorf("project id=%d is already archived", id))
	}
	result := s.db.WithContext(ctx).Model(&domain.Project{}).
		Where("id = ? AND status = ?", id, project.Status).
		Update("status", "archived")
	if result.Error != nil {
		return fmt.Errorf("archive project id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("archive project id=%d affected %d rows", id, result.RowsAffected)
	}
	now := time.Now().UTC()
	if _, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: "project",
		SubjectID:   id,
		Description: fmt.Sprintf("项目从“%s”状态归档。", project.Status),
		OccurredAt:  &now,
		SourceKind:  &factSourceBackground,
	}); err != nil {
		return err
	}
	return nil
}

// ListAll returns every project (no pagination), ordered by priority. It is
// used by the jarvis-tools CLI so codex can scan the full project catalog when
// attributing a Todo to a project.
func (s *ProjectService) ListAll(ctx context.Context) ([]ProjectView, error) {
	items := make([]domain.Project, 0)
	if err := s.db.WithContext(ctx).Where("status <> ?", "archived").Order("priority ASC, id DESC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list all projects: %w", err)
	}
	return toProjectViews(items), nil
}

func (s *ProjectService) List(ctx context.Context, filter ListFilter) (*ProjectList, error) {
	if err := filter.validate(); err != nil {
		return nil, invalid(err)
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&domain.Project{}).Where("status <> ?", "archived").Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count projects: %w", err)
	}
	items := make([]domain.Project, 0, filter.PageSize)
	if total > 0 {
		if err := s.db.WithContext(ctx).
			Where("status <> ?", "archived").
			Order("priority ASC, id DESC").
			Limit(filter.PageSize).
			Offset(filter.offset()).
			Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
	}
	return &ProjectList{Items: toProjectViews(items), Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func projectChangedFields(before *domain.Project, in ProjectInput) []string {
	fields := make([]string, 0, 10)
	if !reflect.DeepEqual(before.Code, in.Code) {
		fields = append(fields, "code")
	}
	if before.Name != in.Name {
		fields = append(fields, "name")
	}
	if before.Role != in.Role {
		fields = append(fields, "role")
	}
	if before.Status != in.Status {
		fields = append(fields, "status")
	}
	if before.Priority != in.Priority {
		fields = append(fields, "priority")
	}
	if !reflect.DeepEqual(before.Description, in.Description) {
		fields = append(fields, "description")
	}
	if !bytes.Equal(before.Repos, in.Repos) {
		fields = append(fields, "repos")
	}
	if !bytes.Equal(before.TechStack, in.TechStack) {
		fields = append(fields, "tech_stack")
	}
	if !bytes.Equal(before.KeyDecisions, in.KeyDecisions) {
		fields = append(fields, "key_decisions")
	}
	if !bytes.Equal(before.Timeline, in.Timeline) {
		fields = append(fields, "timeline")
	}
	if !reflect.DeepEqual(before.Notes, in.Notes) {
		fields = append(fields, "notes")
	}
	return fields
}

func withoutField(fields []string, excluded string) []string {
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != excluded {
			result = append(result, field)
		}
	}
	return result
}
