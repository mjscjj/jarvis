// Package taskcreate owns the source-agnostic creation of executable Tasks.
package taskcreate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/gorm"
	"jarvis/internal/datatypes"
)

const (
	SourceTodo          = "todo"
	SourceScheduledTask = "scheduled_task"
	SourceManual        = "manual"
	SourceProactive     = "proactive"
)

var (
	ErrInvalidInput = errors.New("invalid Task creation input")
	ErrExists       = errors.New("Task already exists for source occurrence")
)

type Input struct {
	TodoID        *uint64
	Title         string
	ActionType    string
	Target        string
	Background    json.RawMessage
	SourcePayload json.RawMessage
	ProjectID     *uint64
	RepoPath      *string
	SourceType    string
	SourceID      *uint64
	OccurrenceKey *string
	ActorType     string
	EventDetail   map[string]any
}

type Factory struct {
	db        *gorm.DB
	assembler *contextsnap.Assembler
	now       func() time.Time
}

func NewFactory(db *gorm.DB, assemblers ...*contextsnap.Assembler) (*Factory, error) {
	if db == nil {
		return nil, fmt.Errorf("Task factory db is nil")
	}
	if len(assemblers) > 1 {
		return nil, fmt.Errorf("Task factory accepts at most one context snapshot assembler")
	}
	var assembler *contextsnap.Assembler
	if len(assemblers) == 1 {
		if assemblers[0] == nil {
			return nil, fmt.Errorf("Task factory context snapshot assembler is nil")
		}
		assembler = assemblers[0]
	}
	return &Factory{db: db, assembler: assembler, now: time.Now}, nil
}

func (f *Factory) Create(ctx context.Context, input Input) (*domain.Task, error) {
	prepared, err := f.assembleBackground(ctx, input)
	if err != nil {
		return nil, err
	}
	var task *domain.Task
	err = f.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created, err := f.CreateWithDB(ctx, tx, prepared)
		if err != nil {
			return err
		}
		task = created
		return nil
	})
	return task, err
}

func (f *Factory) assembleBackground(ctx context.Context, input Input) (Input, error) {
	if input.SourceType == SourceTodo {
		return input, nil
	}
	switch input.SourceType {
	case SourceManual, SourceScheduledTask, SourceProactive:
	default:
		return input, nil
	}
	if f.assembler == nil {
		return Input{}, fmt.Errorf("assemble %s Task background: context snapshot assembler is not configured", input.SourceType)
	}
	options := contextsnap.AssembleOptions{
		ProjectID: input.ProjectID, RequestContext: input.Background,
	}
	var background json.RawMessage
	var err error
	if input.SourceType == SourceScheduledTask {
		background, err = f.assembler.AssembleConversation(ctx, options)
	} else {
		background, err = f.assembler.Assemble(ctx, options)
	}
	if err != nil {
		return Input{}, fmt.Errorf("assemble %s Task background: %w", input.SourceType, err)
	}
	snapshot, err := contextsnap.Decode(background)
	if err != nil {
		return Input{}, fmt.Errorf("validate assembled %s Task background: %w", input.SourceType, err)
	}
	input.Background = background
	if snapshot.Project != nil {
		projectID := snapshot.Project.ID
		input.ProjectID = &projectID
	}
	return input, nil
}

// CreateWithDB lets callers with an existing transaction keep Task creation and
// their own state transition in the same commit.
func (f *Factory) CreateWithDB(ctx context.Context, db *gorm.DB, input Input) (*domain.Task, error) {
	if db == nil {
		return nil, fmt.Errorf("Task factory write db is nil")
	}
	normalized, err := normalizeInput(input)
	if err != nil {
		return nil, err
	}
	if normalized.SourceID != nil && normalized.OccurrenceKey != nil {
		var existing domain.Task
		found := db.WithContext(ctx).
			Where("source_type = ? AND source_id = ? AND occurrence_key = ?", normalized.SourceType, *normalized.SourceID, *normalized.OccurrenceKey).
			Limit(1).Find(&existing)
		if found.Error != nil {
			return nil, fmt.Errorf("check existing Task source=%s/%d occurrence=%s: %w",
				normalized.SourceType, *normalized.SourceID, *normalized.OccurrenceKey, found.Error)
		}
		if found.RowsAffected != 0 {
			return nil, fmt.Errorf("%w: task_id=%d", ErrExists, existing.ID)
		}
	}
	now := f.now().UTC()
	row := domain.Task{
		TodoID: normalized.TodoID, Title: normalized.Title, ActionType: normalized.ActionType,
		Target: normalized.Target, Background: datatypes.JSON(normalized.Background),
		SourcePayload: datatypes.JSON(normalized.SourcePayload),
		SourceType:    normalized.SourceType, SourceID: normalized.SourceID, OccurrenceKey: normalized.OccurrenceKey,
		Status:    "pending",
		ProjectID: normalized.ProjectID, RepoPath: normalized.RepoPath,
		Version: 0, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, fmt.Errorf("%w: source=%s", ErrExists, normalized.SourceType)
		}
		return nil, fmt.Errorf("create Task source=%s: %w", normalized.SourceType, err)
	}
	actorType := strings.TrimSpace(normalized.ActorType)
	if actorType == "" {
		actorType = normalized.SourceType
	}
	if err := progress.AppendTaskEvent(db.WithContext(ctx), progress.TaskEventInput{
		TaskID: row.ID, TaskVersion: row.Version, EventType: "created",
		ToStatus: row.Status, ActorType: actorType, Detail: normalized.EventDetail,
		OccurredAt: now,
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

func normalizeInput(input Input) (Input, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.ActionType = strings.TrimSpace(input.ActionType)
	input.Target = strings.TrimSpace(input.Target)
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.RepoPath = trimString(input.RepoPath)
	if input.Title == "" || input.ActionType == "" || input.Target == "" {
		return Input{}, fmt.Errorf("%w: title, action_type and target are required", ErrInvalidInput)
	}
	switch input.SourceType {
	case SourceTodo, SourceScheduledTask, SourceManual, SourceProactive:
	default:
		return Input{}, fmt.Errorf("%w: source_type must be todo, scheduled_task, manual or proactive", ErrInvalidInput)
	}
	if input.SourceType == SourceTodo {
		if input.TodoID == nil || *input.TodoID == 0 {
			return Input{}, fmt.Errorf("%w: todo source requires todo_id", ErrInvalidInput)
		}
		if input.SourceID == nil {
			input.SourceID = copyUint64(input.TodoID)
		}
	}
	if input.SourceType == SourceScheduledTask {
		if input.SourceID == nil || *input.SourceID == 0 || input.OccurrenceKey == nil || strings.TrimSpace(*input.OccurrenceKey) == "" {
			return Input{}, fmt.Errorf("%w: scheduled_task source requires source_id and occurrence_key", ErrInvalidInput)
		}
	}
	if input.SourceID != nil && *input.SourceID == 0 {
		return Input{}, fmt.Errorf("%w: source_id must be positive", ErrInvalidInput)
	}
	input.Background = mustJSONObject(input.Background, true)
	if input.Background == nil {
		return Input{}, fmt.Errorf("%w: background must be a JSON object", ErrInvalidInput)
	}
	input.SourcePayload = mustJSONValue(input.SourcePayload, true)
	if input.SourcePayload == nil {
		return Input{}, fmt.Errorf("%w: source_payload must be a non-null JSON value", ErrInvalidInput)
	}
	input.OccurrenceKey = trimString(input.OccurrenceKey)
	return input, nil
}

func mustJSONObject(raw []byte, allowEmpty bool) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil || (!allowEmpty && len(object) == 0) {
		return nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil
	}
	return encoded
}

func mustJSONValue(raw []byte, allowEmpty bool) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil
	}
	if !allowEmpty {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				return nil
			}
		case []any:
			if len(typed) == 0 {
				return nil
			}
		case map[string]any:
			if len(typed) == 0 {
				return nil
			}
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func trimString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
