package background

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// PersonList is the paginated response for persons.
type PersonList struct {
	Items    []PersonView `json:"items"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

// PersonService is the authoritative CRUD owner of the person table.
type PersonService struct {
	db *gorm.DB
}

func NewPersonService(db *gorm.DB) (*PersonService, error) {
	if db == nil {
		return nil, fmt.Errorf("person service db is nil")
	}
	return &PersonService{db: db}, nil
}

func (s *PersonService) Create(ctx context.Context, in PersonCreateInput) (*PersonView, error) {
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	person := domain.Person{
		OpenID:         in.OpenID,
		UnionID:        in.UnionID,
		FeishuUserID:   in.FeishuUserID,
		Name:           in.Name,
		EnName:         in.EnName,
		AvatarURL:      in.AvatarURL,
		Department:     in.Department,
		Title:          in.Title,
		Role:           in.Role,
		PriorityWeight: in.PriorityWeight,
		Relation:       in.Relation,
		CommStyle:      in.CommStyle,
		P2PChatID:      in.P2PChatID,
		Notes:          in.Notes,
		IsActive:       in.IsActive == nil || *in.IsActive,
	}
	if err := s.db.WithContext(ctx).Create(&person).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, fmt.Errorf("%w: person open_id=%s", ErrConflict, in.OpenID)
		}
		return nil, fmt.Errorf("create person: %w", err)
	}
	view := toPersonView(&person)
	return &view, nil
}

func (s *PersonService) Get(ctx context.Context, id uint64) (*PersonView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("person id must be positive"))
	}
	var person domain.Person
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&person).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get person id=%d: %w", id, err)
	}
	view := toPersonView(&person)
	return &view, nil
}

// GetByOpenID looks a person up by their Feishu open_id (the business key). It
// is used by the jarvis-tools CLI so codex can resolve a participant to their
// role/relation during extraction.
func (s *PersonService) GetByOpenID(ctx context.Context, openID string) (*PersonView, error) {
	if strings.TrimSpace(openID) == "" {
		return nil, invalid(fmt.Errorf("person open_id must not be empty"))
	}
	var person domain.Person
	err := s.db.WithContext(ctx).Where("open_id = ?", openID).Take(&person).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get person open_id=%s: %w", openID, err)
	}
	view := toPersonView(&person)
	return &view, nil
}

func (s *PersonService) Update(ctx context.Context, id uint64, in PersonUpdateInput) (*PersonView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("person id must be positive"))
	}
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	updates := map[string]any{
		"name":            in.Name,
		"department":      in.Department,
		"title":           in.Title,
		"role":            in.Role,
		"priority_weight": in.PriorityWeight,
		"relation":        in.Relation,
		"comm_style":      in.CommStyle,
		"notes":           in.Notes,
		"is_active":       in.IsActive == nil || *in.IsActive,
	}
	result := s.db.WithContext(ctx).Model(&domain.Person{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update person id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *PersonService) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return invalid(fmt.Errorf("person id must be positive"))
	}
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.Person{})
	if result.Error != nil {
		return fmt.Errorf("delete person id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PersonService) List(ctx context.Context, filter ListFilter) (*PersonList, error) {
	if err := filter.validate(); err != nil {
		return nil, invalid(err)
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&domain.Person{}).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count persons: %w", err)
	}
	items := make([]domain.Person, 0, filter.PageSize)
	if total > 0 {
		if err := s.db.WithContext(ctx).
			Order("priority_weight DESC, id DESC").
			Limit(filter.PageSize).
			Offset(filter.offset()).
			Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list persons: %w", err)
		}
	}
	return &PersonList{Items: toPersonViews(items), Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}
