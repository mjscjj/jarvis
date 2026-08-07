// Package progress owns append-only Task and Project business history.
package progress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
	"jarvis/internal/datatypes"
)

var (
	ErrInvalidInput = errors.New("invalid progress event input")
	ErrNotFound     = errors.New("progress event parent not found")
)

// FactSourceRollup marks a fact written by the daily compression job. Detail
// facts keep their original source_kind (or NULL); the prompt loads the two
// layers separately via SourceKind / ExcludeSourceKind.
const FactSourceRollup = "rollup"

var taskEventTypes = map[string]struct{}{
	"created": {}, "execution_started": {}, "approval_requested": {},
	"approval_granted": {}, "approval_rejected": {}, "rerun_requested": {},
	"reapply_started": {}, "supplemented": {}, "execution_succeeded": {},
	"execution_failed": {}, "execution_observing": {}, "execution_interrupted": {},
	"stale_failed": {}, "stale_requeued": {}, "snapshot_imported": {}, "updated": {}, "closed": {},
	"feishu_message_recalled": {},
	"waiting_scheduled":       {}, "resumed": {}, "human_input_requested": {},
	"human_response_received": {},
}

var actorTypes = map[string]struct{}{
	"user": {}, "m5": {}, "proactive": {}, "scheduled_task": {}, "system": {}, "seed": {}, "migration": {},
}

var taskStatuses = map[string]struct{}{
	"pending": {}, "executing": {}, "waiting": {}, "needs_human": {}, "awaiting_approval": {}, "done": {}, "failed": {}, "observing": {},
}

type TaskEventInput struct {
	TaskID      uint64
	TaskVersion int32
	EventType   string
	FromStatus  *string
	ToStatus    string
	ActorType   string
	ActorRef    *string
	RunID       *uint64
	Detail      any
	OccurredAt  time.Time
}

// FactInput appends one fact. SubjectType is free-form; see domain.Fact.
type FactInput struct {
	SubjectType string     `json:"subject_type"`
	SubjectID   uint64     `json:"subject_id"`
	Description string     `json:"description"`
	OccurredAt  *time.Time `json:"occurred_at"`
	SourceKind  *string    `json:"source_kind"`
	SourceID    *uint64    `json:"source_id"`
}

// FactFilter selects facts for one subject, optionally narrowed to a half-open
// time window. Callers own the timezone: to read a natural day, pass that day's
// local midnight and the next one. Limit caps the newest-first result.
//
// SourceKind restricts to facts written by one producer; ExcludeSourceKind
// removes one. They exist because the prompt needs the two layers separately:
// today's detail is "everything except the rollup", the previous day is
// "the rollup only".
type FactFilter struct {
	SubjectType       string
	SubjectID         uint64
	From              *time.Time
	Until             *time.Time
	Limit             int
	SourceKind        *string
	ExcludeSourceKind *string
}

type TaskEventView struct {
	ID          uint64          `json:"id"`
	TaskID      uint64          `json:"task_id"`
	TaskVersion int32           `json:"task_version"`
	EventType   string          `json:"event_type"`
	FromStatus  *string         `json:"from_status"`
	ToStatus    string          `json:"to_status"`
	ActorType   string          `json:"actor_type"`
	ActorRef    *string         `json:"actor_ref"`
	RunID       *uint64         `json:"run_id"`
	Detail      json.RawMessage `json:"detail"`
	OccurredAt  time.Time       `json:"occurred_at"`
	CreatedAt   time.Time       `json:"created_at"`
}

type FactView struct {
	ID          uint64    `json:"id"`
	SubjectType string    `json:"subject_type"`
	SubjectID   uint64    `json:"subject_id"`
	Description string    `json:"description"`
	OccurredAt  time.Time `json:"occurred_at"`
	SourceKind  *string   `json:"source_kind"`
	SourceID    *uint64   `json:"source_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type EventService interface {
	ListTaskEvents(context.Context, uint64) ([]TaskEventView, error)
	AppendFact(context.Context, FactInput) (*FactView, error)
	ListFacts(context.Context, FactFilter) ([]FactView, error)
}

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func NewService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("progress service db is nil")
	}
	return &Service{db: db, now: time.Now}, nil
}

// AppendTaskEvent writes one event at the same persistence boundary as its Task
// version change. Callers may pass their existing transaction handle.
func AppendTaskEvent(db *gorm.DB, input TaskEventInput) error {
	if db == nil {
		return fmt.Errorf("append task event db is nil")
	}
	prepared, err := prepareTaskEvent(input)
	if err != nil {
		return err
	}
	if err := db.Create(prepared).Error; err != nil {
		return fmt.Errorf("append task event task_id=%d version=%d: %w", input.TaskID, input.TaskVersion, err)
	}
	return nil
}

func (s *Service) ListTaskEvents(ctx context.Context, taskID uint64) ([]TaskEventView, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	if err := requireParent(s.db.WithContext(ctx), &domain.Task{}, taskID); err != nil {
		return nil, err
	}
	var rows []domain.TaskEvent
	if err := s.db.WithContext(ctx).Where("task_id = ?", taskID).
		Order("occurred_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list task events task_id=%d: %w", taskID, err)
	}
	views := make([]TaskEventView, len(rows))
	for i := range rows {
		views[i] = taskEventView(&rows[i])
	}
	return views, nil
}

// AppendFact stores one fact. Subjects whose type the system knows are checked
// for existence so a typo cannot orphan a fact; an unrecognized SubjectType is
// stored as-is, because refusing it would throw away an observation in exchange
// for an enum nobody asked for.
func (s *Service) AppendFact(ctx context.Context, input FactInput) (*FactView, error) {
	if input.OccurredAt == nil {
		now := s.now().UTC()
		input.OccurredAt = &now
	}
	fact, err := prepareFact(input)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	if parent, ok := factSubjectModel(fact.SubjectType); ok {
		if err := requireParent(db, parent, fact.SubjectID); err != nil {
			return nil, err
		}
	}
	// A factengine source unit can be retried after its Agent has already updated
	// Jarvis's internal world model. The source pair is the mechanical replay
	// boundary; exact content from that same unit is returned instead of appended
	// again. Semantic consolidation across different units remains the Agent's job.
	if fact.SourceKind != nil && fact.SourceID != nil {
		var existing domain.Fact
		result := db.Where(
			"source_kind = ? AND source_id = ? AND subject_type = ? AND subject_id = ? AND description = ?",
			*fact.SourceKind, *fact.SourceID, fact.SubjectType, fact.SubjectID, fact.Description,
		).Limit(1).Find(&existing)
		if result.Error != nil {
			return nil, fmt.Errorf("find replayed fact source=%s/%d: %w", *fact.SourceKind, *fact.SourceID, result.Error)
		}
		if result.RowsAffected == 1 {
			view := factView(&existing)
			return &view, nil
		}
	}
	if err := db.Create(fact).Error; err != nil {
		return nil, fmt.Errorf("append fact subject=%s/%d: %w", fact.SubjectType, fact.SubjectID, err)
	}
	if err := db.First(fact, fact.ID).Error; err != nil {
		return nil, fmt.Errorf("reload fact id=%d: %w", fact.ID, err)
	}
	view := factView(fact)
	return &view, nil
}

// ListFacts returns one subject's facts newest first.
func (s *Service) ListFacts(ctx context.Context, filter FactFilter) ([]FactView, error) {
	subjectType := strings.TrimSpace(strings.ToLower(filter.SubjectType))
	if subjectType == "" || filter.SubjectID == 0 {
		return nil, fmt.Errorf("%w: subject_type and positive subject_id are required", ErrInvalidInput)
	}
	query := s.db.WithContext(ctx).Where("subject_type = ? AND subject_id = ?", subjectType, filter.SubjectID)
	if filter.From != nil {
		query = query.Where("occurred_at >= ?", filter.From.UTC())
	}
	if filter.Until != nil {
		query = query.Where("occurred_at < ?", filter.Until.UTC())
	}
	if filter.SourceKind != nil {
		query = query.Where("source_kind = ?", strings.TrimSpace(*filter.SourceKind))
	}
	if filter.ExcludeSourceKind != nil {
		// source_kind is nullable; excluding a value must still return NULL rows.
		query = query.Where("(source_kind IS NULL OR source_kind <> ?)", strings.TrimSpace(*filter.ExcludeSourceKind))
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	var rows []domain.Fact
	if err := query.Order("occurred_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list facts subject=%s/%d: %w", subjectType, filter.SubjectID, err)
	}
	views := make([]FactView, len(rows))
	for i := range rows {
		views[i] = factView(&rows[i])
	}
	return views, nil
}

func prepareTaskEvent(input TaskEventInput) (*domain.TaskEvent, error) {
	input.EventType = strings.TrimSpace(strings.ToLower(input.EventType))
	input.ActorType = strings.TrimSpace(strings.ToLower(input.ActorType))
	input.ToStatus = strings.TrimSpace(strings.ToLower(input.ToStatus))
	input.FromStatus = normalizedOptional(input.FromStatus)
	input.ActorRef = normalizedOptional(input.ActorRef)
	if input.TaskID == 0 || input.TaskVersion < 0 || input.OccurredAt.IsZero() {
		return nil, fmt.Errorf("%w: task_id, non-negative task_version and occurred_at are required", ErrInvalidInput)
	}
	if _, ok := taskEventTypes[input.EventType]; !ok {
		return nil, fmt.Errorf("%w: unsupported task event type %q", ErrInvalidInput, input.EventType)
	}
	if _, ok := actorTypes[input.ActorType]; !ok {
		return nil, fmt.Errorf("%w: unsupported actor type %q", ErrInvalidInput, input.ActorType)
	}
	if _, ok := taskStatuses[input.ToStatus]; !ok {
		return nil, fmt.Errorf("%w: unsupported to_status %q", ErrInvalidInput, input.ToStatus)
	}
	if input.FromStatus != nil {
		if _, ok := taskStatuses[*input.FromStatus]; !ok {
			return nil, fmt.Errorf("%w: unsupported from_status %q", ErrInvalidInput, *input.FromStatus)
		}
	}
	detail, err := encodeDetail(input.Detail)
	if err != nil {
		return nil, err
	}
	return &domain.TaskEvent{
		TaskID: input.TaskID, TaskVersion: input.TaskVersion, EventType: input.EventType,
		FromStatus: input.FromStatus, ToStatus: input.ToStatus, ActorType: input.ActorType,
		ActorRef: input.ActorRef, RunID: input.RunID, Detail: detail,
		OccurredAt: input.OccurredAt.UTC(),
	}, nil
}

func prepareFact(input FactInput) (*domain.Fact, error) {
	input.Description = strings.TrimSpace(input.Description)
	input.SubjectType = strings.TrimSpace(strings.ToLower(input.SubjectType))
	input.SourceKind = normalizedOptional(input.SourceKind)
	if input.SubjectType == "" || input.SubjectID == 0 || input.Description == "" ||
		input.OccurredAt == nil || input.OccurredAt.IsZero() {
		return nil, fmt.Errorf("%w: subject_type, subject_id, description and occurred_at are required", ErrInvalidInput)
	}
	if input.SourceID != nil && (*input.SourceID == 0 || input.SourceKind == nil) {
		return nil, fmt.Errorf("%w: source_id must be positive and requires source_kind", ErrInvalidInput)
	}
	return &domain.Fact{
		SubjectType: input.SubjectType, SubjectID: input.SubjectID,
		Description: input.Description, OccurredAt: input.OccurredAt.UTC(),
		SourceKind: input.SourceKind, SourceID: input.SourceID,
	}, nil
}

// factSubjectModel maps the subject types the system reads back to their tables.
// A miss is not an error; see AppendFact.
func factSubjectModel(subjectType string) (any, bool) {
	switch subjectType {
	case "project":
		return &domain.Project{}, true
	case "key_matter":
		return &domain.KeyMatter{}, true
	case "group":
		return &domain.Group{}, true
	case "person":
		return &domain.Person{}, true
	case "task":
		return &domain.Task{}, true
	default:
		return nil, false
	}
}

func requireParent(db *gorm.DB, model any, id uint64) error {
	var count int64
	if err := db.Model(model).Where("id = ?", id).Count(&count).Error; err != nil {
		return fmt.Errorf("check progress event parent id=%d: %w", id, err)
	}
	if count != 1 {
		return fmt.Errorf("%w: id=%d", ErrNotFound, id)
	}
	return nil
}

func encodeDetail(value any) (datatypes.JSON, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: encode event detail: %v", ErrInvalidInput, err)
	}
	return datatypes.JSON(encoded), nil
}

func taskEventView(event *domain.TaskEvent) TaskEventView {
	return TaskEventView{
		ID: event.ID, TaskID: event.TaskID, TaskVersion: event.TaskVersion,
		EventType: event.EventType, FromStatus: event.FromStatus, ToStatus: event.ToStatus,
		ActorType: event.ActorType, ActorRef: event.ActorRef, RunID: event.RunID,
		Detail: rawJSON(event.Detail), OccurredAt: event.OccurredAt, CreatedAt: event.CreatedAt,
	}
}

func factView(fact *domain.Fact) FactView {
	return FactView{
		ID: fact.ID, SubjectType: fact.SubjectType, SubjectID: fact.SubjectID,
		Description: fact.Description, OccurredAt: fact.OccurredAt,
		SourceKind: fact.SourceKind, SourceID: fact.SourceID, CreatedAt: fact.CreatedAt,
	}
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), value...))
}

func normalizedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
