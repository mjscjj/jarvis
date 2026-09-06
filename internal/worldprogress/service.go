// Package worldprogress persists Jarvis's evidence-backed periodic assessments.
// An assessment is neither an objective Fact nor an external product's official
// progress record. Optional modules validate their own subject references.
package worldprogress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var (
	ErrInvalidInput       = errors.New("invalid world progress input")
	ErrNotFound           = errors.New("world progress not found")
	ErrConflict           = errors.New("world progress version conflict")
	ErrSubjectUnavailable = errors.New("world progress subject unavailable")
)

var (
	typeToken   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	periodToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	entityRef   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}:[^[:space:]]{1,128}$`)
)

var signals = map[string]struct{}{
	"unknown": {},
	"green":   {},
	"yellow":  {},
	"red":     {},
}

// SubjectValidator is the narrow adapter from the core assessment store to an
// optional subject owner. It runs only for writes, so historical assessments
// stay readable while their module is disabled.
type SubjectValidator func(context.Context, string, string) error

type CreateInput struct {
	ExpectedVersion *int32         `json:"expected_version"`
	SubjectType     string         `json:"subject_type"`
	SubjectID       string         `json:"subject_id"`
	PeriodKey       string         `json:"period_key"`
	Signal          string         `json:"signal"`
	Summary         string         `json:"summary"`
	Evidence        datatypes.JSON `json:"evidence"`
	EvidenceUntil   *time.Time     `json:"evidence_until"`
}

type UpdateInput struct {
	ExpectedVersion *int32         `json:"expected_version"`
	Signal          string         `json:"signal"`
	Summary         string         `json:"summary"`
	Evidence        datatypes.JSON `json:"evidence"`
	EvidenceUntil   *time.Time     `json:"evidence_until"`
}

type Filter struct {
	SubjectType string
	SubjectID   string
	PeriodKey   string
}

type View struct {
	ID            uint64          `json:"id"`
	SubjectType   string          `json:"subject_type"`
	SubjectID     string          `json:"subject_id"`
	PeriodKey     string          `json:"period_key"`
	Signal        string          `json:"signal"`
	Summary       string          `json:"summary"`
	Evidence      json.RawMessage `json:"evidence"`
	Version       int32           `json:"version"`
	AssessedAt    time.Time       `json:"assessed_at"`
	EvidenceUntil time.Time       `json:"evidence_until"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type AssessmentService interface {
	Get(context.Context, uint64) (*View, error)
	GetBySubjectPeriod(context.Context, Filter) (*View, error)
	Create(context.Context, CreateInput) (*View, error)
	Update(context.Context, uint64, UpdateInput) (*View, error)
}

type Service struct {
	db              *gorm.DB
	validateSubject SubjectValidator
	now             func() time.Time
}

func NewService(db *gorm.DB, validateSubject SubjectValidator) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("world progress service db is nil")
	}
	if validateSubject == nil {
		return nil, fmt.Errorf("world progress subject validator is nil")
	}
	return &Service{db: db, validateSubject: validateSubject, now: time.Now}, nil
}

func (s *Service) Get(ctx context.Context, id uint64) (*View, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	var row domain.WorldProgress
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, mapLookupError(err, fmt.Sprintf("id=%d", id))
	}
	view := toView(&row)
	return &view, nil
}

func (s *Service) GetBySubjectPeriod(ctx context.Context, filter Filter) (*View, error) {
	filter = normalizeFilter(filter)
	if err := validateFilter(filter); err != nil {
		return nil, err
	}
	var row domain.WorldProgress
	err := s.db.WithContext(ctx).Where(
		"subject_type = ? AND subject_id = ? AND period_key = ?",
		filter.SubjectType, filter.SubjectID, filter.PeriodKey,
	).First(&row).Error
	if err != nil {
		return nil, mapLookupError(err, fmt.Sprintf("subject=%s:%s period=%s", filter.SubjectType, filter.SubjectID, filter.PeriodKey))
	}
	view := toView(&row)
	return &view, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*View, error) {
	prepared, err := prepareCreate(input)
	if err != nil {
		return nil, err
	}
	if err := s.validateSubject(ctx, prepared.SubjectType, prepared.SubjectID); err != nil {
		return nil, fmt.Errorf("validate world progress subject %s:%s: %w", prepared.SubjectType, prepared.SubjectID, err)
	}
	now := s.now().UTC()
	prepared.AssessedAt = now
	prepared.CreatedAt = now
	prepared.UpdatedAt = now
	if err := s.db.WithContext(ctx).Create(prepared).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, fmt.Errorf("%w: subject=%s:%s period=%s", ErrConflict, prepared.SubjectType, prepared.SubjectID, prepared.PeriodKey)
		}
		return nil, fmt.Errorf("create world progress: %w", err)
	}
	view := toView(prepared)
	return &view, nil
}

func (s *Service) Update(ctx context.Context, id uint64, input UpdateInput) (*View, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	prepared, err := prepareUpdate(input)
	if err != nil {
		return nil, err
	}
	var current domain.WorldProgress
	if err := s.db.WithContext(ctx).First(&current, id).Error; err != nil {
		return nil, mapLookupError(err, fmt.Sprintf("id=%d", id))
	}
	if current.Version != *input.ExpectedVersion {
		return nil, fmt.Errorf("%w: id=%d expected=%d current=%d", ErrConflict, id, *input.ExpectedVersion, current.Version)
	}
	if err := s.validateSubject(ctx, current.SubjectType, current.SubjectID); err != nil {
		return nil, fmt.Errorf("validate world progress subject %s:%s: %w", current.SubjectType, current.SubjectID, err)
	}
	if sameAssessment(&current, prepared) {
		view := toView(&current)
		return &view, nil
	}
	now := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.WorldProgress{}).
		Where("id = ? AND version = ?", id, *input.ExpectedVersion).
		Updates(map[string]any{
			"signal": prepared.Signal, "summary": prepared.Summary, "evidence": prepared.Evidence,
			"evidence_until": prepared.EvidenceUntil, "assessed_at": now,
			"updated_at": now, "version": gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("update world progress id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: id=%d expected=%d", ErrConflict, id, *input.ExpectedVersion)
	}
	if err := s.db.WithContext(ctx).First(&current, id).Error; err != nil {
		return nil, fmt.Errorf("reload world progress id=%d: %w", id, err)
	}
	view := toView(&current)
	return &view, nil
}

func prepareCreate(input CreateInput) (*domain.WorldProgress, error) {
	if input.ExpectedVersion == nil || *input.ExpectedVersion != 0 {
		return nil, fmt.Errorf("%w: expected_version must be zero when creating", ErrInvalidInput)
	}
	filter := normalizeFilter(Filter{SubjectType: input.SubjectType, SubjectID: input.SubjectID, PeriodKey: input.PeriodKey})
	if err := validateFilter(filter); err != nil {
		return nil, err
	}
	common, err := prepareAssessment(input.Signal, input.Summary, input.Evidence, input.EvidenceUntil)
	if err != nil {
		return nil, err
	}
	return &domain.WorldProgress{
		SubjectType: filter.SubjectType, SubjectID: filter.SubjectID, PeriodKey: filter.PeriodKey,
		Signal: common.Signal, Summary: common.Summary, Evidence: common.Evidence,
		EvidenceUntil: common.EvidenceUntil, Version: 0,
	}, nil
}

func prepareUpdate(input UpdateInput) (*domain.WorldProgress, error) {
	if input.ExpectedVersion == nil || *input.ExpectedVersion < 0 {
		return nil, fmt.Errorf("%w: non-negative expected_version is required", ErrInvalidInput)
	}
	return prepareAssessment(input.Signal, input.Summary, input.Evidence, input.EvidenceUntil)
}

func prepareAssessment(signal, summary string, evidence datatypes.JSON, evidenceUntil *time.Time) (*domain.WorldProgress, error) {
	signal = strings.ToLower(strings.TrimSpace(signal))
	summary = strings.TrimSpace(summary)
	if _, ok := signals[signal]; !ok {
		return nil, fmt.Errorf("%w: signal must be unknown, green, yellow, or red", ErrInvalidInput)
	}
	if summary == "" {
		return nil, fmt.Errorf("%w: summary is required", ErrInvalidInput)
	}
	if evidenceUntil == nil || evidenceUntil.IsZero() {
		return nil, fmt.Errorf("%w: evidence_until is required", ErrInvalidInput)
	}
	canonical, err := canonicalEvidence(evidence)
	if err != nil {
		return nil, err
	}
	return &domain.WorldProgress{Signal: signal, Summary: summary, Evidence: canonical, EvidenceUntil: evidenceUntil.UTC()}, nil
}

func canonicalEvidence(value datatypes.JSON) (datatypes.JSON, error) {
	if len(bytes.TrimSpace(value)) == 0 {
		return nil, fmt.Errorf("%w: evidence must be a JSON object", ErrInvalidInput)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%w: evidence must be a JSON object", ErrInvalidInput)
	}
	if raw, ok := object["refs"]; ok {
		var refs []string
		if err := json.Unmarshal(raw, &refs); err != nil {
			return nil, fmt.Errorf("%w: evidence.refs must be an array of entity references", ErrInvalidInput)
		}
		for _, ref := range refs {
			if !entityRef.MatchString(strings.TrimSpace(ref)) {
				return nil, fmt.Errorf("%w: invalid evidence ref %q", ErrInvalidInput, ref)
			}
		}
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("%w: encode evidence: %v", ErrInvalidInput, err)
	}
	return datatypes.JSON(encoded), nil
}

func normalizeFilter(filter Filter) Filter {
	filter.SubjectType = strings.ToLower(strings.TrimSpace(filter.SubjectType))
	filter.SubjectID = strings.TrimSpace(filter.SubjectID)
	filter.PeriodKey = strings.TrimSpace(filter.PeriodKey)
	return filter
}

func validateFilter(filter Filter) error {
	if !typeToken.MatchString(filter.SubjectType) {
		return fmt.Errorf("%w: subject_type must be a lowercase type token", ErrInvalidInput)
	}
	if filter.SubjectID == "" || len(filter.SubjectID) > 128 || strings.ContainsAny(filter.SubjectID, "\r\n\t") {
		return fmt.Errorf("%w: subject_id must be a non-empty stable ID", ErrInvalidInput)
	}
	if !periodToken.MatchString(filter.PeriodKey) {
		return fmt.Errorf("%w: period_key must be a stable period token", ErrInvalidInput)
	}
	return nil
}

func sameAssessment(current *domain.WorldProgress, prepared *domain.WorldProgress) bool {
	return current.Signal == prepared.Signal && current.Summary == prepared.Summary &&
		bytes.Equal(current.Evidence, prepared.Evidence) && current.EvidenceUntil.Equal(prepared.EvidenceUntil)
}

func mapLookupError(err error, identity string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %s", ErrNotFound, identity)
	}
	return fmt.Errorf("read world progress %s: %w", identity, err)
}

func toView(row *domain.WorldProgress) View {
	evidence := json.RawMessage(append([]byte(nil), row.Evidence...))
	if len(evidence) == 0 {
		evidence = json.RawMessage("{}")
	}
	return View{
		ID: row.ID, SubjectType: row.SubjectType, SubjectID: row.SubjectID, PeriodKey: row.PeriodKey,
		Signal: row.Signal, Summary: row.Summary, Evidence: evidence, Version: row.Version,
		AssessedAt: row.AssessedAt, EvidenceUntil: row.EvidenceUntil, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
