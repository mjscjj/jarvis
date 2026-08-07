package background

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/gorm"
)

// KeyMatterList is the paginated response for key matters.
type KeyMatterList struct {
	Items    []KeyMatterView `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	MaxOpen  int             `json:"max_open"`
}

const maxOpenKeyMatters = 10

// KeyMatterFilter controls whether closed matters are included.
type KeyMatterFilter struct {
	ListFilter
	IncludeClosed bool
}

// KeyMatterService is the authoritative CRUD owner of the key_matter table.
type KeyMatterService struct {
	db     *gorm.DB
	events *progress.Service
	now    func() time.Time
	mu     sync.Mutex
}

func NewKeyMatterService(db *gorm.DB) (*KeyMatterService, error) {
	if db == nil {
		return nil, fmt.Errorf("key matter service db is nil")
	}
	events, err := progress.NewService(db)
	if err != nil {
		return nil, err
	}
	return &KeyMatterService{db: db, events: events, now: time.Now}, nil
}

func (s *KeyMatterService) Create(ctx context.Context, in KeyMatterInput) (*KeyMatterView, error) {
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := s.requireProject(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireOpenCapacity(ctx); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	matter := domain.KeyMatter{
		Title: in.Title, Status: in.Status, Summary: in.Summary,
		ProjectID: in.ProjectID, DueAt: in.DueAt, LastActiveAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&matter).Error; err != nil {
		return nil, fmt.Errorf("create key matter: %w", err)
	}
	occurredAt := matter.CreatedAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if _, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: "key_matter", SubjectID: matter.ID,
		Description: fmt.Sprintf("立项关键事项“%s”，当前状态为“%s”。", matter.Title, matter.Status),
		OccurredAt:  &occurredAt, SourceKind: &factSourceBackground,
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, matter.ID)
}

// Touch marks one open matter as freshly relevant without rewriting its
// semantic fields or manufacturing a progress fact.
func (s *KeyMatterService) Touch(ctx context.Context, id uint64) (*KeyMatterView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("key matter id must be positive"))
	}
	var matter domain.KeyMatter
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&matter).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load key matter id=%d before touch: %w", id, err)
	}
	if matter.ClosedAt != nil {
		return nil, invalid(fmt.Errorf("key matter id=%d is closed", id))
	}
	activeAt := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.KeyMatter{}).Where("id = ? AND closed_at IS NULL", id).
		UpdateColumn("last_active_at", activeAt)
	if result.Error != nil {
		return nil, fmt.Errorf("touch key matter id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("touch key matter id=%d affected %d rows", id, result.RowsAffected)
	}
	return s.Get(ctx, id)
}

func (s *KeyMatterService) Get(ctx context.Context, id uint64) (*KeyMatterView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("key matter id must be positive"))
	}
	var matter domain.KeyMatter
	err := s.db.WithContext(ctx).Preload("Project").Where("id = ?", id).Take(&matter).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get key matter id=%d: %w", id, err)
	}
	view := toKeyMatterView(&matter)
	return &view, nil
}

func (s *KeyMatterService) Update(ctx context.Context, id uint64, in KeyMatterInput) (*KeyMatterView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("key matter id must be positive"))
	}
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := s.requireProject(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	var before domain.KeyMatter
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&before).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load key matter id=%d before update: %w", id, err)
	}
	changedFields := keyMatterChangedFields(&before, in)
	if len(changedFields) == 0 {
		return s.Get(ctx, id)
	}
	updates := map[string]any{
		"title": in.Title, "status": in.Status, "summary": in.Summary,
		"project_id": in.ProjectID, "due_at": in.DueAt,
	}
	summaryChanged := containsField(changedFields, "summary")
	now := time.Now().UTC()
	if summaryChanged {
		updates["last_progress_at"] = now
	}
	result := s.db.WithContext(ctx).Model(&domain.KeyMatter{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update key matter id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("update key matter id=%d affected %d rows", id, result.RowsAffected)
	}
	if summaryChanged {
		description := "清空关键事项当前进展。"
		if in.Summary != nil {
			description = fmt.Sprintf("关键事项当前进展更新为：%s。", *in.Summary)
		}
		if _, err := s.events.AppendFact(ctx, progress.FactInput{
			SubjectType: "key_matter", SubjectID: id, Description: description,
			OccurredAt: &now, SourceKind: &factSourceBackground,
		}); err != nil {
			return nil, err
		}
	}
	profileFields := withoutField(changedFields, "summary")
	if len(profileFields) > 0 {
		if _, err := s.events.AppendFact(ctx, progress.FactInput{
			SubjectType: "key_matter", SubjectID: id,
			Description: fmt.Sprintf("更新关键事项资料：%s。", strings.Join(profileFields, "、")),
			OccurredAt:  &now, SourceKind: &factSourceBackground,
		}); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, id)
}

func (s *KeyMatterService) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return invalid(fmt.Errorf("key matter id must be positive"))
	}
	var matter domain.KeyMatter
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&matter).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("load key matter id=%d before close: %w", id, err)
	}
	if matter.ClosedAt != nil {
		return invalid(fmt.Errorf("key matter id=%d is already closed", id))
	}
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.KeyMatter{}).
		Where("id = ? AND closed_at IS NULL", id).Update("closed_at", now)
	if result.Error != nil {
		return fmt.Errorf("close key matter id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("close key matter id=%d affected %d rows", id, result.RowsAffected)
	}
	if _, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: "key_matter", SubjectID: id,
		Description: fmt.Sprintf("关键事项“%s”已闭环。", matter.Title),
		OccurredAt:  &now, SourceKind: &factSourceBackground,
	}); err != nil {
		return err
	}
	return nil
}

func (s *KeyMatterService) ListAll(ctx context.Context) ([]KeyMatterView, error) {
	items := make([]domain.KeyMatter, 0)
	if err := s.openQuery(ctx).Preload("Project").Order(keyMatterOrder).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list all key matters: %w", err)
	}
	return toKeyMatterViews(items), nil
}

func (s *KeyMatterService) List(ctx context.Context, filter KeyMatterFilter) (*KeyMatterList, error) {
	if err := filter.ListFilter.validate(); err != nil {
		return nil, invalid(err)
	}
	query := s.db.WithContext(ctx).Model(&domain.KeyMatter{})
	if !filter.IncludeClosed {
		query = query.Where("closed_at IS NULL")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count key matters: %w", err)
	}
	items := make([]domain.KeyMatter, 0, filter.PageSize)
	if total > 0 {
		query = s.db.WithContext(ctx).Preload("Project")
		if !filter.IncludeClosed {
			query = query.Where("closed_at IS NULL")
		}
		if err := query.Order(keyMatterOrder).Limit(filter.PageSize).Offset(filter.offset()).Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list key matters: %w", err)
		}
	}
	return &KeyMatterList{
		Items: toKeyMatterViews(items), Total: total, Page: filter.Page, PageSize: filter.PageSize,
		MaxOpen: maxOpenKeyMatters,
	}, nil
}

// SQLite stores legacy timestamps with mixed timezone suffixes. datetime()
// compares their actual instants instead of their textual representations.
const keyMatterOrder = "datetime(last_active_at) DESC, id DESC"

func (s *KeyMatterService) openQuery(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Where("closed_at IS NULL")
}

func (s *KeyMatterService) requireProject(ctx context.Context, projectID *uint64) error {
	if projectID == nil {
		return nil
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&domain.Project{}).Where("id = ?", *projectID).Count(&count).Error; err != nil {
		return fmt.Errorf("verify project id=%d: %w", *projectID, err)
	}
	if count != 1 {
		return invalid(fmt.Errorf("key matter project_id=%d does not exist", *projectID))
	}
	return nil
}

func (s *KeyMatterService) requireOpenCapacity(ctx context.Context) error {
	var count int64
	if err := s.openQuery(ctx).Model(&domain.KeyMatter{}).Count(&count).Error; err != nil {
		return fmt.Errorf("count open key matters before create: %w", err)
	}
	if count >= maxOpenKeyMatters {
		return invalid(fmt.Errorf("open key matter limit reached: %d", maxOpenKeyMatters))
	}
	return nil
}

func keyMatterChangedFields(before *domain.KeyMatter, in KeyMatterInput) []string {
	fields := make([]string, 0, 5)
	if before.Title != in.Title {
		fields = append(fields, "title")
	}
	if before.Status != in.Status {
		fields = append(fields, "status")
	}
	if !sameOptionalString(before.Summary, in.Summary) {
		fields = append(fields, "summary")
	}
	if !sameOptionalUint64(before.ProjectID, in.ProjectID) {
		fields = append(fields, "project_id")
	}
	if !sameOptionalTime(before.DueAt, in.DueAt) {
		fields = append(fields, "due_at")
	}
	return fields
}

func containsField(fields []string, target string) bool {
	for _, field := range fields {
		if field == target {
			return true
		}
	}
	return false
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameOptionalUint64(left, right *uint64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
