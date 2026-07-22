// Package knowledge owns sourced, time-bounded relationships between existing
// Jarvis domain entities. It deliberately does not introduce a generic entity
// registry: type + primary key resolve directly to the authoritative tables.
package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrInvalidInput = errors.New("invalid relation fact input")
	ErrNotFound     = errors.New("relation fact not found")
	ErrNotActive    = errors.New("relation fact is not active")
)

type EntityType string

const (
	EntityProject         EntityType = "project"
	EntityPerson          EntityType = "person"
	EntityPrincipal       EntityType = "principal"
	EntityGroup           EntityType = "group"
	EntityTodo            EntityType = "todo"
	EntityTask            EntityType = "task"
	EntityResource        EntityType = "resource"
	EntityManagedResource EntityType = "managed_resource"
)

var validEntityTypes = map[EntityType]struct{}{
	EntityProject: {}, EntityPerson: {}, EntityPrincipal: {}, EntityGroup: {},
	EntityTodo: {}, EntityTask: {}, EntityResource: {}, EntityManagedResource: {},
}

type EntityRef struct {
	Type EntityType `json:"type"`
	ID   uint64     `json:"id"`
}

type CreateInput struct {
	Subject       EntityRef       `json:"subject"`
	Predicate     string          `json:"predicate"`
	Object        *EntityRef      `json:"object"`
	Value         json.RawMessage `json:"value"`
	AssertionKind string          `json:"assertion_kind"`
	Confidence    *float64        `json:"confidence"`
	ValidFrom     *time.Time      `json:"valid_from"`
	ValidTo       *time.Time      `json:"valid_to"`
	SourceType    string          `json:"source_type"`
	SourceID      string          `json:"source_id"`
	SourceQuote   *string         `json:"source_quote"`
	Model         *string         `json:"model"`
	PromptVersion *string         `json:"prompt_version"`
}

type RetractInput struct {
	FactID uint64
	By     string
	Reason string
}

type FactFilter struct {
	SubjectType     *EntityType
	SubjectID       *uint64
	ObjectType      *EntityType
	ObjectID        *uint64
	Predicate       string
	IncludeInactive bool
	AsOf            time.Time
	Page            int
	PageSize        int
}

type FactView struct {
	ID               uint64          `json:"id"`
	Subject          EntityRef       `json:"subject"`
	Predicate        string          `json:"predicate"`
	Object           *EntityRef      `json:"object,omitempty"`
	Value            json.RawMessage `json:"value"`
	AssertionKind    string          `json:"assertion_kind"`
	Confidence       *float64        `json:"confidence"`
	ValidFrom        *time.Time      `json:"valid_from"`
	ValidTo          *time.Time      `json:"valid_to"`
	Status           string          `json:"status"`
	SupersededByID   *uint64         `json:"superseded_by_id"`
	RetractedAt      *time.Time      `json:"retracted_at"`
	RetractedBy      *string         `json:"retracted_by"`
	RetractionReason *string         `json:"retraction_reason"`
	SourceType       string          `json:"source_type"`
	SourceID         string          `json:"source_id"`
	SourceQuote      *string         `json:"source_quote"`
	Model            *string         `json:"model"`
	PromptVersion    *string         `json:"prompt_version"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
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
	Retract(context.Context, RetractInput) (*FactView, error)
}

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func NewService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("knowledge service db is nil")
	}
	return &Service{db: db, now: time.Now}, nil
}

type preparedCreate struct {
	input    CreateInput
	spec     predicateSpec
	value    datatypes.JSON
	dedupKey string
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*FactView, error) {
	prepared, err := prepareCreate(input)
	if err != nil {
		return nil, err
	}
	if err := s.requireEntity(ctx, prepared.input.Subject); err != nil {
		return nil, fmt.Errorf("validate relation subject: %w", err)
	}
	if prepared.input.Object != nil {
		if err := s.requireEntity(ctx, *prepared.input.Object); err != nil {
			return nil, fmt.Errorf("validate relation object: %w", err)
		}
	}

	var existing domain.RelationFact
	find := s.db.WithContext(ctx).Where("dedup_key = ?", prepared.dedupKey).Limit(1).Find(&existing)
	if find.Error != nil {
		return nil, fmt.Errorf("check relation fact dedup: %w", find.Error)
	}
	if find.RowsAffected == 1 {
		// A retry of an already superseded/retracted fact must be a pure read.
		// Re-applying supersede from an old fact could incorrectly close its newer
		// replacement. An active row may retry supersede to repair an earlier
		// post-insert failure in this deliberately non-transactional write path.
		if existing.Status == "active" {
			if err := s.supersedePrevious(ctx, &existing, prepared.spec); err != nil {
				return nil, err
			}
		}
		view := factView(&existing)
		return &view, nil
	}

	now := s.now().UTC()
	fact := domain.RelationFact{
		SubjectType: string(prepared.input.Subject.Type), SubjectID: prepared.input.Subject.ID,
		Predicate: prepared.input.Predicate, AssertionKind: prepared.input.AssertionKind,
		Confidence: prepared.input.Confidence, ValidFrom: utcPointer(prepared.input.ValidFrom),
		ValidTo: utcPointer(prepared.input.ValidTo), Status: "active",
		SourceType: prepared.input.SourceType, SourceID: prepared.input.SourceID,
		SourceQuote: prepared.input.SourceQuote, Model: prepared.input.Model,
		PromptVersion: prepared.input.PromptVersion, ValueJSON: prepared.value,
		DedupKey: prepared.dedupKey, CreatedAt: now, UpdatedAt: now,
	}
	if prepared.input.Object != nil {
		objectType := string(prepared.input.Object.Type)
		objectID := prepared.input.Object.ID
		fact.ObjectType = &objectType
		fact.ObjectID = &objectID
	}
	if err := s.db.WithContext(ctx).Create(&fact).Error; err != nil {
		return nil, fmt.Errorf("create relation fact: %w", err)
	}
	if err := s.supersedePrevious(ctx, &fact, prepared.spec); err != nil {
		return nil, err
	}
	view := factView(&fact)
	return &view, nil
}

func (s *Service) supersedePrevious(ctx context.Context, current *domain.RelationFact, spec predicateSpec) error {
	if spec.cardinality != cardinalityOne {
		return nil
	}
	if current.ValidFrom == nil {
		return fmt.Errorf("%w: single-valued predicate %q requires valid_from", ErrInvalidInput, current.Predicate)
	}
	result := s.db.WithContext(ctx).Model(&domain.RelationFact{}).
		Where("subject_type = ? AND subject_id = ? AND predicate = ? AND status = ? AND id <> ?", current.SubjectType, current.SubjectID, current.Predicate, "active", current.ID).
		Where("valid_to IS NULL OR valid_to > ?", *current.ValidFrom).
		Updates(map[string]any{
			"status": "superseded", "valid_to": current.ValidFrom,
			"superseded_by_id": current.ID,
		})
	if result.Error != nil {
		return fmt.Errorf("supersede previous relation facts current_id=%d: %w", current.ID, result.Error)
	}
	return nil
}

func (s *Service) List(ctx context.Context, filter FactFilter) (*FactList, error) {
	filter.Predicate = strings.TrimSpace(strings.ToLower(filter.Predicate))
	if filter.SubjectType != nil {
		normalized := EntityType(strings.TrimSpace(string(*filter.SubjectType)))
		filter.SubjectType = &normalized
	}
	if filter.ObjectType != nil {
		normalized := EntityType(strings.TrimSpace(string(*filter.ObjectType)))
		filter.ObjectType = &normalized
	}
	if err := validateFilter(filter); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&domain.RelationFact{})
	if filter.SubjectType != nil {
		query = query.Where("subject_type = ? AND subject_id = ?", string(*filter.SubjectType), *filter.SubjectID)
	}
	if filter.ObjectType != nil {
		query = query.Where("object_type = ? AND object_id = ?", string(*filter.ObjectType), *filter.ObjectID)
	}
	if filter.Predicate != "" {
		query = query.Where("predicate = ?", filter.Predicate)
	}
	if !filter.IncludeInactive {
		asOf := filter.AsOf
		if asOf.IsZero() {
			asOf = s.now().UTC()
		}
		query = query.Where("status = ?", "active").
			Where("valid_from IS NULL OR valid_from <= ?", asOf).
			Where("valid_to IS NULL OR valid_to > ?", asOf)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count relation facts: %w", err)
	}
	var rows []domain.RelationFact
	if err := query.Order("created_at DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list relation facts: %w", err)
	}
	items := make([]FactView, len(rows))
	for i := range rows {
		items[i] = factView(&rows[i])
	}
	return &FactList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *Service) Retract(ctx context.Context, input RetractInput) (*FactView, error) {
	by := strings.TrimSpace(input.By)
	reason := strings.TrimSpace(input.Reason)
	if input.FactID == 0 || by == "" || reason == "" {
		return nil, fmt.Errorf("%w: fact_id, by and reason are required", ErrInvalidInput)
	}
	var fact domain.RelationFact
	if err := s.db.WithContext(ctx).First(&fact, input.FactID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load relation fact id=%d: %w", input.FactID, err)
	}
	if fact.Status != "active" {
		return nil, fmt.Errorf("%w: fact_id=%d status=%s", ErrNotActive, fact.ID, fact.Status)
	}
	now := s.now().UTC()
	validTo := fact.ValidTo
	if validTo == nil || validTo.After(now) {
		validTo = &now
	}
	result := s.db.WithContext(ctx).Model(&domain.RelationFact{}).
		Where("id = ? AND status = ?", fact.ID, "active").
		Updates(map[string]any{
			"status": "retracted", "valid_to": validTo, "retracted_at": now,
			"retracted_by": by, "retraction_reason": reason,
		})
	if result.Error != nil {
		return nil, fmt.Errorf("retract relation fact id=%d: %w", fact.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: fact_id=%d changed concurrently", ErrNotActive, fact.ID)
	}
	if err := s.db.WithContext(ctx).First(&fact, fact.ID).Error; err != nil {
		return nil, fmt.Errorf("reload retracted relation fact id=%d: %w", fact.ID, err)
	}
	view := factView(&fact)
	return &view, nil
}

func (s *Service) HasReferences(ctx context.Context, entity EntityRef) (bool, error) {
	if err := validateEntityRef(entity); err != nil {
		return false, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&domain.RelationFact{}).
		Where("(subject_type = ? AND subject_id = ?) OR (object_type = ? AND object_id = ?)", string(entity.Type), entity.ID, string(entity.Type), entity.ID).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("count relation references type=%s id=%d: %w", entity.Type, entity.ID, err)
	}
	return count > 0, nil
}

func (s *Service) requireEntity(ctx context.Context, ref EntityRef) error {
	if err := validateEntityRef(ref); err != nil {
		return err
	}
	var model any
	switch ref.Type {
	case EntityProject:
		model = &domain.Project{}
	case EntityPerson:
		model = &domain.Person{}
	case EntityPrincipal:
		model = &domain.PrincipalProfile{}
	case EntityGroup:
		model = &domain.Group{}
	case EntityTodo:
		model = &domain.Todo{}
	case EntityTask:
		model = &domain.Task{}
	case EntityResource:
		model = &domain.Resource{}
	case EntityManagedResource:
		model = &domain.ManagedResource{}
	default:
		return fmt.Errorf("%w: unsupported entity type %q", ErrInvalidInput, ref.Type)
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

func prepareCreate(input CreateInput) (*preparedCreate, error) {
	input.Subject.Type = EntityType(strings.TrimSpace(string(input.Subject.Type)))
	input.Predicate = strings.TrimSpace(strings.ToLower(input.Predicate))
	input.AssertionKind = strings.TrimSpace(strings.ToLower(input.AssertionKind))
	input.SourceType = strings.TrimSpace(strings.ToLower(input.SourceType))
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.SourceQuote = trimOptional(input.SourceQuote)
	input.Model = trimOptional(input.Model)
	input.PromptVersion = trimOptional(input.PromptVersion)
	if input.Object != nil {
		input.Object.Type = EntityType(strings.TrimSpace(string(input.Object.Type)))
	}
	if err := validateEntityRef(input.Subject); err != nil {
		return nil, err
	}
	spec, err := lookupPredicate(input.Predicate)
	if err != nil {
		return nil, err
	}
	if _, ok := spec.subjectTypes[input.Subject.Type]; !ok {
		return nil, fmt.Errorf("%w: predicate %q does not accept subject type %q", ErrInvalidInput, input.Predicate, input.Subject.Type)
	}
	if input.SourceType == "" || input.SourceID == "" {
		return nil, fmt.Errorf("%w: source_type and source_id are required", ErrInvalidInput)
	}
	if input.AssertionKind != "system" && input.AssertionKind != "manual" && input.AssertionKind != "inferred" {
		return nil, fmt.Errorf("%w: assertion_kind must be system, manual or inferred", ErrInvalidInput)
	}
	if input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1) {
		return nil, fmt.Errorf("%w: confidence must be between 0 and 1", ErrInvalidInput)
	}
	if input.AssertionKind == "inferred" {
		if input.Confidence == nil || input.Model == nil || input.PromptVersion == nil {
			return nil, fmt.Errorf("%w: inferred fact requires confidence, model and prompt_version", ErrInvalidInput)
		}
	}
	if input.ValidFrom != nil && input.ValidTo != nil && !input.ValidTo.After(*input.ValidFrom) {
		return nil, fmt.Errorf("%w: valid_to must be after valid_from", ErrInvalidInput)
	}
	if spec.cardinality == cardinalityOne && input.ValidFrom == nil {
		return nil, fmt.Errorf("%w: single-valued predicate %q requires valid_from", ErrInvalidInput, input.Predicate)
	}

	var value datatypes.JSON
	switch spec.target {
	case targetEntity:
		if input.Object == nil || len(bytes.TrimSpace(input.Value)) != 0 {
			return nil, fmt.Errorf("%w: predicate %q requires object and forbids value", ErrInvalidInput, input.Predicate)
		}
		if err := validateEntityRef(*input.Object); err != nil {
			return nil, err
		}
		if _, ok := spec.objectTypes[input.Object.Type]; !ok {
			return nil, fmt.Errorf("%w: predicate %q does not accept object type %q", ErrInvalidInput, input.Predicate, input.Object.Type)
		}
	case targetValue:
		if input.Object != nil {
			return nil, fmt.Errorf("%w: predicate %q requires value and forbids object", ErrInvalidInput, input.Predicate)
		}
		canonical, err := canonicalJSONValue(input.Value)
		if err != nil {
			return nil, err
		}
		value = datatypes.JSON(canonical)
	default:
		return nil, fmt.Errorf("predicate %q has invalid target configuration", input.Predicate)
	}
	dedupKey, err := relationDedupKey(input, value)
	if err != nil {
		return nil, err
	}
	return &preparedCreate{input: input, spec: spec, value: value, dedupKey: dedupKey}, nil
}

func validateFilter(filter FactFilter) error {
	if filter.Page <= 0 || filter.PageSize <= 0 || filter.PageSize > 100 {
		return fmt.Errorf("%w: page must be positive and page_size must be between 1 and 100", ErrInvalidInput)
	}
	if (filter.SubjectType == nil) != (filter.SubjectID == nil) {
		return fmt.Errorf("%w: subject_type and subject_id must be provided together", ErrInvalidInput)
	}
	if filter.SubjectType != nil {
		if err := validateEntityRef(EntityRef{Type: *filter.SubjectType, ID: *filter.SubjectID}); err != nil {
			return err
		}
	}
	if (filter.ObjectType == nil) != (filter.ObjectID == nil) {
		return fmt.Errorf("%w: object_type and object_id must be provided together", ErrInvalidInput)
	}
	if filter.ObjectType != nil {
		if err := validateEntityRef(EntityRef{Type: *filter.ObjectType, ID: *filter.ObjectID}); err != nil {
			return err
		}
	}
	if filter.Predicate != "" {
		if _, err := lookupPredicate(filter.Predicate); err != nil {
			return err
		}
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

func canonicalJSONValue(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: value is required", ErrInvalidInput)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: decode value: %v", ErrInvalidInput, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%w: value must not be null", ErrInvalidInput)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: value must contain one JSON value", ErrInvalidInput)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode relation fact value: %w", err)
	}
	return encoded, nil
}

func relationDedupKey(input CreateInput, value datatypes.JSON) (string, error) {
	payload := struct {
		Subject    EntityRef       `json:"subject"`
		Predicate  string          `json:"predicate"`
		Object     *EntityRef      `json:"object,omitempty"`
		Value      json.RawMessage `json:"value,omitempty"`
		ValidFrom  string          `json:"valid_from,omitempty"`
		ValidTo    string          `json:"valid_to,omitempty"`
		SourceType string          `json:"source_type"`
		SourceID   string          `json:"source_id"`
	}{
		Subject: input.Subject, Predicate: input.Predicate, Object: input.Object,
		Value: json.RawMessage(value), SourceType: input.SourceType, SourceID: input.SourceID,
	}
	if input.ValidFrom != nil {
		payload.ValidFrom = input.ValidFrom.UTC().Format(time.RFC3339Nano)
	}
	if input.ValidTo != nil {
		payload.ValidTo = input.ValidTo.UTC().Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode relation fact dedup payload: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func factView(fact *domain.RelationFact) FactView {
	view := FactView{
		ID: fact.ID, Subject: EntityRef{Type: EntityType(fact.SubjectType), ID: fact.SubjectID},
		Predicate: fact.Predicate, Value: json.RawMessage("null"),
		AssertionKind: fact.AssertionKind, Confidence: fact.Confidence,
		ValidFrom: fact.ValidFrom, ValidTo: fact.ValidTo, Status: fact.Status,
		SupersededByID: fact.SupersededByID, RetractedAt: fact.RetractedAt,
		RetractedBy: fact.RetractedBy, RetractionReason: fact.RetractionReason,
		SourceType: fact.SourceType, SourceID: fact.SourceID, SourceQuote: fact.SourceQuote,
		Model: fact.Model, PromptVersion: fact.PromptVersion,
		CreatedAt: fact.CreatedAt, UpdatedAt: fact.UpdatedAt,
	}
	if fact.ObjectType != nil && fact.ObjectID != nil {
		view.Object = &EntityRef{Type: EntityType(*fact.ObjectType), ID: *fact.ObjectID}
	}
	if len(fact.ValueJSON) != 0 {
		view.Value = json.RawMessage(append([]byte(nil), fact.ValueJSON...))
	}
	return view
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
