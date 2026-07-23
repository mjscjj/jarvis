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
	"execution_failed": {}, "execution_interrupted": {}, "stale_failed": {}, "snapshot_imported": {},
	"waiting_scheduled": {}, "resumed": {}, "human_input_requested": {},
	"human_response_received": {},
}

var actorTypes = map[string]struct{}{
	"user": {}, "m4": {}, "m5": {}, "scheduled_task": {}, "system": {}, "seed": {}, "migration": {},
}

var taskStatuses = map[string]struct{}{
	"pending": {}, "executing": {}, "waiting": {}, "needs_human": {}, "awaiting_approval": {}, "done": {}, "failed": {},
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
	ProjectID   uint64     `json:"-"`
	Description string     `json:"description"`
	OccurredAt  *time.Time `json:"occurred_at"`
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
	ID          uint64    `json:"id"`
	ProjectID   uint64    `json:"project_id"`
	Description string    `json:"description"`
	OccurredAt  time.Time `json:"occurred_at"`
	CreatedAt   time.Time `json:"created_at"`
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
	if err := s.db.WithContext(ctx).Create(event).Error; err != nil {
		return nil, fmt.Errorf("append project event project_id=%d: %w", input.ProjectID, err)
	}
	if err := s.db.WithContext(ctx).First(event, event.ID).Error; err != nil {
		return nil, fmt.Errorf("reload project event id=%d: %w", event.ID, err)
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
	input.Description = strings.TrimSpace(input.Description)
	if input.ProjectID == 0 || input.Description == "" || input.OccurredAt == nil || input.OccurredAt.IsZero() {
		return nil, fmt.Errorf("%w: project_id, description and occurred_at are required", ErrInvalidInput)
	}
	return &domain.ProjectEvent{
		ProjectID: input.ProjectID, Description: input.Description,
		OccurredAt: input.OccurredAt.UTC(),
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
		ID: event.ID, ProjectID: event.ProjectID, Description: event.Description,
		OccurredAt: event.OccurredAt, CreatedAt: event.CreatedAt,
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
