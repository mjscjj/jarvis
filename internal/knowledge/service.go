// Package knowledge owns natural-language relationships between existing
// Jarvis domain entities. Entity identity is structured; relationship meaning
// stays in one description that humans and models can read directly.
package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var (
	ErrInvalidInput = errors.New("invalid relation fact input")
	ErrNotFound     = errors.New("relation fact not found")
)

type EntityType string

const (
	EntityProject         EntityType = "project"
	EntityKeyMatter       EntityType = "key_matter"
	EntityPerson          EntityType = "person"
	EntityPrincipal       EntityType = "principal"
	EntityGroup           EntityType = "group"
	EntityTodo            EntityType = "todo"
	EntityTask            EntityType = "task"
	EntityResource        EntityType = "resource"
	EntityManagedResource EntityType = "managed_resource"
)

var validEntityTypes = map[EntityType]struct{}{
	EntityProject: {}, EntityKeyMatter: {}, EntityPerson: {}, EntityPrincipal: {}, EntityGroup: {},
	EntityTodo: {}, EntityTask: {}, EntityResource: {}, EntityManagedResource: {},
}

type EntityRef struct {
	Type  EntityType `json:"type"`
	ID    uint64     `json:"id"`
	Label string     `json:"label,omitempty"`
}

type CreateInput struct {
	EntityA     EntityRef  `json:"entity_a"`
	EntityB     EntityRef  `json:"entity_b"`
	Description string     `json:"description"`
	ValidFrom   *time.Time `json:"valid_from"`
	ValidUntil  *time.Time `json:"valid_until"`
}

// UpdateInput replaces the whole editable payload: an omitted ValidFrom or
// ValidUntil clears that bound rather than leaving the stored value alone.
type UpdateInput struct {
	FactID      uint64     `json:"-"`
	Description string     `json:"description"`
	ValidFrom   *time.Time `json:"valid_from"`
	ValidUntil  *time.Time `json:"valid_until"`
}

type FactFilter struct {
	EntityType *EntityType
	EntityID   *uint64
	Page       int
	PageSize   int
}

type FactView struct {
	ID          uint64     `json:"id"`
	EntityA     EntityRef  `json:"entity_a"`
	EntityB     EntityRef  `json:"entity_b"`
	Description string     `json:"description"`
	ValidFrom   *time.Time `json:"valid_from"`
	ValidUntil  *time.Time `json:"valid_until"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type FactList struct {
	Items    []FactView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type FactService interface {
	Create(context.Context, CreateInput) (*FactView, error)
	List(context.Context, FactFilter) (*FactList, error)
	Update(context.Context, UpdateInput) (*FactView, error)
	Delete(context.Context, uint64) error
}

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("knowledge service db is nil")
	}
	return &Service{db: db}, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*FactView, error) {
	prepared, err := prepareCreate(input)
	if err != nil {
		return nil, err
	}
	if err := s.requireEntity(ctx, prepared.EntityA); err != nil {
		return nil, fmt.Errorf("validate relation entity_a: %w", err)
	}
	if err := s.requireEntity(ctx, prepared.EntityB); err != nil {
		return nil, fmt.Errorf("validate relation entity_b: %w", err)
	}

	var fact domain.RelationFact
	find := s.db.WithContext(ctx).
		Where("entity_a_type = ? AND entity_a_id = ? AND entity_b_type = ? AND entity_b_id = ?",
			prepared.EntityA.Type, prepared.EntityA.ID, prepared.EntityB.Type, prepared.EntityB.ID).
		Limit(1).Find(&fact)
	if find.Error != nil {
		return nil, fmt.Errorf("find relation fact pair: %w", find.Error)
	}
	if find.RowsAffected == 1 {
		if fact.Description != prepared.Description ||
			!sameInstant(fact.ValidFrom, prepared.ValidFrom) ||
			!sameInstant(fact.ValidUntil, prepared.ValidUntil) {
			if err := s.db.WithContext(ctx).Model(&domain.RelationFact{}).
				Where("id = ?", fact.ID).Updates(map[string]any{
				"description": prepared.Description,
				"valid_from":  prepared.ValidFrom,
				"valid_until": prepared.ValidUntil,
			}).Error; err != nil {
				return nil, fmt.Errorf("update relation fact pair id=%d: %w", fact.ID, err)
			}
			var reloaded domain.RelationFact
			if err := s.db.WithContext(ctx).First(&reloaded, fact.ID).Error; err != nil {
				return nil, fmt.Errorf("reload relation fact id=%d: %w", fact.ID, err)
			}
			fact = reloaded
		}
		return s.factView(ctx, &fact)
	}

	fact = domain.RelationFact{
		EntityAType: string(prepared.EntityA.Type), EntityAID: prepared.EntityA.ID,
		EntityBType: string(prepared.EntityB.Type), EntityBID: prepared.EntityB.ID,
		Description: prepared.Description,
		ValidFrom:   prepared.ValidFrom,
		ValidUntil:  prepared.ValidUntil,
	}
	if err := s.db.WithContext(ctx).Create(&fact).Error; err != nil {
		return nil, fmt.Errorf("create relation fact: %w", err)
	}
	if err := s.db.WithContext(ctx).First(&fact, fact.ID).Error; err != nil {
		return nil, fmt.Errorf("reload relation fact id=%d: %w", fact.ID, err)
	}
	return s.factView(ctx, &fact)
}

func (s *Service) List(ctx context.Context, filter FactFilter) (*FactList, error) {
	if filter.EntityType != nil {
		normalized := EntityType(strings.TrimSpace(string(*filter.EntityType)))
		filter.EntityType = &normalized
	}
	if err := validateFilter(filter); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&domain.RelationFact{})
	if filter.EntityType != nil {
		query = query.Where(
			"(entity_a_type = ? AND entity_a_id = ?) OR (entity_b_type = ? AND entity_b_id = ?)",
			*filter.EntityType, *filter.EntityID, *filter.EntityType, *filter.EntityID,
		)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count relation facts: %w", err)
	}
	var rows []domain.RelationFact
	if err := query.Order("updated_at DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list relation facts: %w", err)
	}
	items := make([]FactView, len(rows))
	for i := range rows {
		view, err := s.factView(ctx, &rows[i])
		if err != nil {
			return nil, err
		}
		items[i] = *view
	}
	return &FactList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (*FactView, error) {
	description := strings.TrimSpace(input.Description)
	if input.FactID == 0 || description == "" {
		return nil, fmt.Errorf("%w: fact_id and description are required", ErrInvalidInput)
	}
	validFrom, validUntil, err := normalizePeriod(input.ValidFrom, input.ValidUntil)
	if err != nil {
		return nil, err
	}
	var fact domain.RelationFact
	if err := s.db.WithContext(ctx).First(&fact, input.FactID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load relation fact id=%d: %w", input.FactID, err)
	}
	if fact.Description != description ||
		!sameInstant(fact.ValidFrom, validFrom) ||
		!sameInstant(fact.ValidUntil, validUntil) {
		if err := s.db.WithContext(ctx).Model(&domain.RelationFact{}).
			Where("id = ?", fact.ID).Updates(map[string]any{
			"description": description,
			"valid_from":  validFrom,
			"valid_until": validUntil,
		}).Error; err != nil {
			return nil, fmt.Errorf("update relation fact id=%d: %w", fact.ID, err)
		}
		var reloaded domain.RelationFact
		if err := s.db.WithContext(ctx).First(&reloaded, fact.ID).Error; err != nil {
			return nil, fmt.Errorf("reload relation fact id=%d: %w", fact.ID, err)
		}
		fact = reloaded
	}
	return s.factView(ctx, &fact)
}

func (s *Service) Delete(ctx context.Context, factID uint64) error {
	if factID == 0 {
		return fmt.Errorf("%w: fact_id must be positive", ErrInvalidInput)
	}
	result := s.db.WithContext(ctx).Delete(&domain.RelationFact{}, factID)
	if result.Error != nil {
		return fmt.Errorf("delete relation fact id=%d: %w", factID, result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func prepareCreate(input CreateInput) (*CreateInput, error) {
	input.EntityA.Type = EntityType(strings.TrimSpace(string(input.EntityA.Type)))
	input.EntityB.Type = EntityType(strings.TrimSpace(string(input.EntityB.Type)))
	input.Description = strings.TrimSpace(input.Description)
	if err := validateEntityRef(input.EntityA); err != nil {
		return nil, err
	}
	if err := validateEntityRef(input.EntityB); err != nil {
		return nil, err
	}
	if input.EntityA.Type == input.EntityB.Type && input.EntityA.ID == input.EntityB.ID {
		return nil, fmt.Errorf("%w: relation entities must be different", ErrInvalidInput)
	}
	if input.Description == "" {
		return nil, fmt.Errorf("%w: description is required", ErrInvalidInput)
	}
	validFrom, validUntil, err := normalizePeriod(input.ValidFrom, input.ValidUntil)
	if err != nil {
		return nil, err
	}
	input.ValidFrom, input.ValidUntil = validFrom, validUntil
	if entityKey(input.EntityB) < entityKey(input.EntityA) {
		input.EntityA, input.EntityB = input.EntityB, input.EntityA
	}
	return &input, nil
}

// normalizePeriod stores both bounds in UTC and rejects an inverted range. A
// nil bound stays nil: the two ends are independent, so "start unknown" and
// "still current" are both representable.
func normalizePeriod(validFrom, validUntil *time.Time) (*time.Time, *time.Time, error) {
	if validFrom != nil {
		utc := validFrom.UTC()
		validFrom = &utc
	}
	if validUntil != nil {
		utc := validUntil.UTC()
		validUntil = &utc
	}
	if validFrom != nil && validUntil != nil && validUntil.Before(*validFrom) {
		return nil, nil, fmt.Errorf("%w: valid_until %s is before valid_from %s",
			ErrInvalidInput, validUntil.Format(time.RFC3339), validFrom.Format(time.RFC3339))
	}
	return validFrom, validUntil, nil
}

func sameInstant(stored, incoming *time.Time) bool {
	if stored == nil || incoming == nil {
		return stored == nil && incoming == nil
	}
	return stored.Equal(*incoming)
}

func validateFilter(filter FactFilter) error {
	if filter.Page <= 0 || filter.PageSize <= 0 || filter.PageSize > 100 {
		return fmt.Errorf("%w: page must be positive and page_size must be between 1 and 100", ErrInvalidInput)
	}
	if (filter.EntityType == nil) != (filter.EntityID == nil) {
		return fmt.Errorf("%w: entity_type and entity_id must be provided together", ErrInvalidInput)
	}
	if filter.EntityType != nil {
		return validateEntityRef(EntityRef{Type: *filter.EntityType, ID: *filter.EntityID})
	}
	return nil
}

func validateEntityRef(ref EntityRef) error {
	if ref.ID == 0 {
		return fmt.Errorf("%w: entity id must be positive", ErrInvalidInput)
	}
	if _, ok := validEntityTypes[ref.Type]; !ok {
		return fmt.Errorf("%w: unsupported entity type %q", ErrInvalidInput, ref.Type)
	}
	return nil
}

func entityKey(ref EntityRef) string {
	return fmt.Sprintf("%s:%020d", ref.Type, ref.ID)
}

func (s *Service) requireEntity(ctx context.Context, ref EntityRef) error {
	if err := validateEntityRef(ref); err != nil {
		return err
	}
	model, err := entityModel(ref.Type)
	if err != nil {
		return err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(model).Where("id = ?", ref.ID).Count(&count).Error; err != nil {
		return fmt.Errorf("query entity type=%s id=%d: %w", ref.Type, ref.ID, err)
	}
	if count != 1 {
		return fmt.Errorf("%w: entity type=%s id=%d does not exist", ErrInvalidInput, ref.Type, ref.ID)
	}
	return nil
}

func entityModel(entityType EntityType) (any, error) {
	switch entityType {
	case EntityProject:
		return &domain.Project{}, nil
	case EntityKeyMatter:
		return &domain.KeyMatter{}, nil
	case EntityPerson:
		return &domain.Person{}, nil
	case EntityPrincipal:
		return &domain.PrincipalProfile{}, nil
	case EntityGroup:
		return &domain.Group{}, nil
	case EntityTodo:
		return &domain.Todo{}, nil
	case EntityTask:
		return &domain.Task{}, nil
	case EntityResource:
		return &domain.Resource{}, nil
	case EntityManagedResource:
		return &domain.ManagedResource{}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported entity type %q", ErrInvalidInput, entityType)
	}
}

func (s *Service) factView(ctx context.Context, fact *domain.RelationFact) (*FactView, error) {
	entityA := EntityRef{Type: EntityType(fact.EntityAType), ID: fact.EntityAID}
	entityB := EntityRef{Type: EntityType(fact.EntityBType), ID: fact.EntityBID}
	var err error
	entityA.Label, err = s.entityLabel(ctx, entityA)
	if err != nil {
		return nil, fmt.Errorf("resolve relation fact id=%d entity_a: %w", fact.ID, err)
	}
	entityB.Label, err = s.entityLabel(ctx, entityB)
	if err != nil {
		return nil, fmt.Errorf("resolve relation fact id=%d entity_b: %w", fact.ID, err)
	}
	return &FactView{
		ID: fact.ID, EntityA: entityA, EntityB: entityB,
		Description: fact.Description,
		ValidFrom:   fact.ValidFrom, ValidUntil: fact.ValidUntil,
		CreatedAt: fact.CreatedAt, UpdatedAt: fact.UpdatedAt,
	}, nil
}

func (s *Service) entityLabel(ctx context.Context, ref EntityRef) (string, error) {
	db := s.db.WithContext(ctx)
	switch ref.Type {
	case EntityProject:
		var row domain.Project
		if err := db.Select("id", "name").First(&row, ref.ID).Error; err != nil {
			return "", err
		}
		return row.Name, nil
	case EntityKeyMatter:
		var row domain.KeyMatter
		if err := db.Select("id", "title").First(&row, ref.ID).Error; err != nil {
			return "", err
		}
		return row.Title, nil
	case EntityPerson:
		var row domain.Person
		if err := db.Select("id", "name").First(&row, ref.ID).Error; err != nil {
			return "", err
		}
		return row.Name, nil
	case EntityPrincipal:
		var row domain.PrincipalProfile
		if err := db.Select("id", "name").First(&row, ref.ID).Error; err != nil {
			return "", err
		}
		return row.Name, nil
	case EntityGroup:
		var row domain.Group
		if err := db.Select("id", "name", "chat_id").First(&row, ref.ID).Error; err != nil {
			return "", err
		}
		if row.Name != nil && strings.TrimSpace(*row.Name) != "" {
			return strings.TrimSpace(*row.Name), nil
		}
		return row.ChatID, nil
	case EntityTodo:
		var row domain.Todo
		if err := db.Select("id", "title").First(&row, ref.ID).Error; err != nil {
			return "", err
		}
		return row.Title, nil
	case EntityTask:
		var row domain.Task
		if err := db.Select("id", "title").First(&row, ref.ID).Error; err != nil {
			return "", err
		}
		return row.Title, nil
	case EntityResource:
		var row domain.Resource
		if err := db.Select("id", "name", "url").First(&row, ref.ID).Error; err != nil {
			return "", err
		}
		if row.Name != nil && strings.TrimSpace(*row.Name) != "" {
			return strings.TrimSpace(*row.Name), nil
		}
		if row.URL != nil && strings.TrimSpace(*row.URL) != "" {
			return strings.TrimSpace(*row.URL), nil
		}
		return fmt.Sprintf("resource:%d", row.ID), nil
	case EntityManagedResource:
		var row domain.ManagedResource
		if err := db.Select("id", "title").First(&row, ref.ID).Error; err != nil {
			return "", err
		}
		return row.Title, nil
	default:
		return "", fmt.Errorf("%w: unsupported entity type %q", ErrInvalidInput, ref.Type)
	}
}
