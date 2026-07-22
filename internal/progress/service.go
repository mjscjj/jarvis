// Package progress owns append-only Task and Project business history.
package progress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrInvalidInput = errors.New("invalid progress event input")
	ErrNotFound     = errors.New("progress event parent not found")
)

var taskEventTypes = map[string]struct{}{
	"created": {}, "execution_started": {}, "approval_requested": {},
	"approval_granted": {}, "approval_rejected": {}, "rerun_requested": {},
	"reapply_started": {}, "supplemented": {}, "execution_succeeded": {},
	"execution_failed": {}, "stale_failed": {}, "snapshot_imported": {},
}

var projectEventTypes = map[string]struct{}{
	"created": {}, "profile_updated": {}, "status_changed": {},
	"progress_reported": {}, "milestone_reached": {}, "decision_recorded": {},
	"blocked": {}, "unblocked": {}, "delivered": {}, "archived": {},
}

var actorTypes = map[string]struct{}{
	"user": {}, "m4": {}, "m5": {}, "system": {}, "seed": {}, "migration": {},
}

var taskStatuses = map[string]struct{}{
	"pending": {}, "executing": {}, "awaiting_approval": {}, "done": {}, "failed": {},
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

type ProjectEventInput struct {
	ProjectID  uint64          `json:"-"`
	EventType  string          `json:"event_type"`
	Title      string          `json:"title"`
	Summary    *string         `json:"summary"`
	FromStatus *string         `json:"from_status"`
	ToStatus   *string         `json:"to_status"`
	ActorType  string          `json:"actor_type"`
	ActorRef   *string         `json:"actor_ref"`
	SourceType *string         `json:"source_type"`
	SourceID   *string         `json:"source_id"`
	Detail     json.RawMessage `json:"detail"`
	EventKey   string          `json:"event_key"`
	OccurredAt *time.Time      `json:"occurred_at"`
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

type ProjectEventView struct {
	ID         uint64          `json:"id"`
	ProjectID  uint64          `json:"project_id"`
	EventType  string          `json:"event_type"`
	Title      string          `json:"title"`
	Summary    *string         `json:"summary"`
	FromStatus *string         `json:"from_status"`
	ToStatus   *string         `json:"to_status"`
	ActorType  string          `json:"actor_type"`
	ActorRef   *string         `json:"actor_ref"`
	SourceType *string         `json:"source_type"`
	SourceID   *string         `json:"source_id"`
	Detail     json.RawMessage `json:"detail"`
	EventKey   string          `json:"event_key"`
	OccurredAt time.Time       `json:"occurred_at"`
	CreatedAt  time.Time       `json:"created_at"`
}

type EventService interface {
	ListTaskEvents(context.Context, uint64) ([]TaskEventView, error)
	AppendProjectEvent(context.Context, ProjectEventInput) (*ProjectEventView, error)
	ListProjectEvents(context.Context, uint64) ([]ProjectEventView, error)
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

func (s *Service) AppendProjectEvent(ctx context.Context, input ProjectEventInput) (*ProjectEventView, error) {
	if input.OccurredAt == nil {
		now := s.now().UTC()
		input.OccurredAt = &now
	}
	event, err := prepareProjectEvent(input)
	if err != nil {
		return nil, err
	}
	if err := requireParent(s.db.WithContext(ctx), &domain.Project{}, input.ProjectID); err != nil {
		return nil, err
	}
	var existing domain.ProjectEvent
	found := s.db.WithContext(ctx).Where("event_key = ?", event.EventKey).Limit(1).Find(&existing)
	if found.Error != nil {
		return nil, fmt.Errorf("check project event key: %w", found.Error)
	}
	if found.RowsAffected == 1 {
		view := projectEventView(&existing)
		return &view, nil
	}
	if err := s.db.WithContext(ctx).Create(event).Error; err != nil {
		return nil, fmt.Errorf("append project event project_id=%d: %w", input.ProjectID, err)
	}
	view := projectEventView(event)
	return &view, nil
}

func (s *Service) ListProjectEvents(ctx context.Context, projectID uint64) ([]ProjectEventView, error) {
	if projectID == 0 {
		return nil, fmt.Errorf("%w: project_id must be positive", ErrInvalidInput)
	}
	if err := requireParent(s.db.WithContext(ctx), &domain.Project{}, projectID); err != nil {
		return nil, err
	}
	var rows []domain.ProjectEvent
	if err := s.db.WithContext(ctx).Where("project_id = ?", projectID).
		Order("occurred_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list project events project_id=%d: %w", projectID, err)
	}
	views := make([]ProjectEventView, len(rows))
	for i := range rows {
		views[i] = projectEventView(&rows[i])
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

func prepareProjectEvent(input ProjectEventInput) (*domain.ProjectEvent, error) {
	input.EventType = strings.TrimSpace(strings.ToLower(input.EventType))
	input.Title = strings.TrimSpace(input.Title)
	input.ActorType = strings.TrimSpace(strings.ToLower(input.ActorType))
	input.ActorRef = normalizedOptional(input.ActorRef)
	input.SourceType = normalizedOptionalLower(input.SourceType)
	input.SourceID = normalizedOptional(input.SourceID)
	input.FromStatus = normalizedOptionalLower(input.FromStatus)
	input.ToStatus = normalizedOptionalLower(input.ToStatus)
	input.Summary = normalizedOptional(input.Summary)
	input.EventKey = strings.TrimSpace(input.EventKey)
	if input.ProjectID == 0 || input.Title == "" || input.OccurredAt == nil || input.OccurredAt.IsZero() {
		return nil, fmt.Errorf("%w: project_id, title and occurred_at are required", ErrInvalidInput)
	}
	if _, ok := projectEventTypes[input.EventType]; !ok {
		return nil, fmt.Errorf("%w: unsupported project event type %q", ErrInvalidInput, input.EventType)
	}
	if _, ok := actorTypes[input.ActorType]; !ok {
		return nil, fmt.Errorf("%w: unsupported actor type %q", ErrInvalidInput, input.ActorType)
	}
	if (input.SourceType == nil) != (input.SourceID == nil) {
		return nil, fmt.Errorf("%w: source_type and source_id must be provided together", ErrInvalidInput)
	}
	if input.EventType == "status_changed" && (input.FromStatus == nil || input.ToStatus == nil) {
		return nil, fmt.Errorf("%w: status_changed requires from_status and to_status", ErrInvalidInput)
	}
	detail, err := canonicalOptionalJSON(input.Detail)
	if err != nil {
		return nil, err
	}
	if input.EventKey == "" {
		input.EventKey, err = projectEventKey(input, detail)
		if err != nil {
			return nil, err
		}
	}
	return &domain.ProjectEvent{
		ProjectID: input.ProjectID, EventType: input.EventType, Title: input.Title,
		Summary: input.Summary, FromStatus: input.FromStatus, ToStatus: input.ToStatus,
		ActorType: input.ActorType, ActorRef: input.ActorRef,
		SourceType: input.SourceType, SourceID: input.SourceID, Detail: detail,
		EventKey: input.EventKey, OccurredAt: input.OccurredAt.UTC(),
	}, nil
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

func canonicalOptionalJSON(raw json.RawMessage) (datatypes.JSON, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%w: decode event detail: %v", ErrInvalidInput, err)
	}
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode event detail: %w", err)
	}
	return datatypes.JSON(encoded), nil
}

func projectEventKey(input ProjectEventInput, detail datatypes.JSON) (string, error) {
	payload := struct {
		ProjectID  uint64          `json:"project_id"`
		EventType  string          `json:"event_type"`
		Title      string          `json:"title"`
		Summary    *string         `json:"summary,omitempty"`
		FromStatus *string         `json:"from_status,omitempty"`
		ToStatus   *string         `json:"to_status,omitempty"`
		ActorType  string          `json:"actor_type"`
		ActorRef   *string         `json:"actor_ref,omitempty"`
		SourceType *string         `json:"source_type,omitempty"`
		SourceID   *string         `json:"source_id,omitempty"`
		Detail     json.RawMessage `json:"detail,omitempty"`
		OccurredAt string          `json:"occurred_at"`
	}{
		ProjectID: input.ProjectID, EventType: input.EventType, Title: input.Title,
		Summary: input.Summary, FromStatus: input.FromStatus, ToStatus: input.ToStatus,
		ActorType: input.ActorType, ActorRef: input.ActorRef,
		SourceType: input.SourceType, SourceID: input.SourceID,
		Detail: json.RawMessage(detail), OccurredAt: input.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode project event key: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func taskEventView(event *domain.TaskEvent) TaskEventView {
	return TaskEventView{
		ID: event.ID, TaskID: event.TaskID, TaskVersion: event.TaskVersion,
		EventType: event.EventType, FromStatus: event.FromStatus, ToStatus: event.ToStatus,
		ActorType: event.ActorType, ActorRef: event.ActorRef, RunID: event.RunID,
		Detail: rawJSON(event.Detail), OccurredAt: event.OccurredAt, CreatedAt: event.CreatedAt,
	}
}

func projectEventView(event *domain.ProjectEvent) ProjectEventView {
	return ProjectEventView{
		ID: event.ID, ProjectID: event.ProjectID, EventType: event.EventType,
		Title: event.Title, Summary: event.Summary, FromStatus: event.FromStatus,
		ToStatus: event.ToStatus, ActorType: event.ActorType, ActorRef: event.ActorRef,
		SourceType: event.SourceType, SourceID: event.SourceID, Detail: rawJSON(event.Detail),
		EventKey: event.EventKey, OccurredAt: event.OccurredAt, CreatedAt: event.CreatedAt,
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

func normalizedOptionalLower(value *string) *string {
	normalized := normalizedOptional(value)
	if normalized == nil {
		return nil
	}
	lower := strings.ToLower(*normalized)
	return &lower
}
