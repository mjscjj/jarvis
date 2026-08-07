package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"jarvis/internal/datatypes"
)

// TodoExtractWatermark is M3's independent per-chat extraction cursor. M2's
// chat_checkpoint must never be reused for this purpose.
type TodoExtractWatermark struct {
	ChatID               string    `gorm:"column:chat_id;primaryKey"`
	LastScannedMessageID string    `gorm:"column:last_scanned_message_id;not null"`
	LastScannedAt        time.Time `gorm:"column:last_scanned_at;not null"`
	UpdatedAt            time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (TodoExtractWatermark) TableName() string { return "todo_extract_watermark" }

// ExtractionRun is M3's append-only record of one chat extraction attempt.
// Counts describe the successfully persisted output; timing and failures keep
// the operational history visible even when extraction stops early.
type ExtractionRun struct {
	ID           uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	ChatID       string `gorm:"column:chat_id;not null;index:idx_extraction_run_chat"`
	Status       string `gorm:"column:status;not null;index:idx_extraction_run_status"`
	MessageCount int64  `gorm:"column:message_count;not null;default:0"`
	TodoCount    int64  `gorm:"column:todo_count;not null;default:0"`

	InputTokens           *int64  `gorm:"column:input_tokens"`
	CachedInputTokens     *int64  `gorm:"column:cached_input_tokens"`
	OutputTokens          *int64  `gorm:"column:output_tokens"`
	ReasoningOutputTokens *int64  `gorm:"column:reasoning_output_tokens"`
	ErrorDetail           *string `gorm:"column:error_detail"`

	StartedAt  time.Time  `gorm:"column:started_at;not null;index:idx_extraction_run_started"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
	DurationMs *int64     `gorm:"column:duration_ms"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
}

func (ExtractionRun) TableName() string { return "extraction_run" }

// TodoEvent is the append-only audit stream shared by M3 and later lifecycle
// owners. M3 writes actor=m3 and never moves a Todo beyond extracted.
type TodoEvent struct {
	ID         uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	TodoID     uint64         `gorm:"column:todo_id;not null;index:idx_todo_event_todo"`
	FromStatus *string        `gorm:"column:from_status"`
	ToStatus   string         `gorm:"column:to_status;not null"`
	Actor      string         `gorm:"column:actor;not null"`
	Detail     datatypes.JSON `gorm:"column:detail"`
	Snapshot   datatypes.JSON `gorm:"column:snapshot"` // 事件发生时的不可变 Todo 语义快照；历史事件不回读当前 Todo 行
	CreatedAt  time.Time      `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`

	Todo *Todo `gorm:"foreignKey:TodoID;constraint:OnDelete:CASCADE"`
}

func (TodoEvent) TableName() string { return "todo_event" }

// TodoEventSnapshot preserves the semantic fields needed to explain one Todo
// event without reading a later version of the mutable Todo row.
type TodoEventSnapshot struct {
	Title              string         `json:"title"`
	Target             string         `json:"target"`
	ProjectID          *uint64        `json:"project_id,omitempty"`
	CommitmentStrength string         `json:"commitment_strength"`
	LeaderAssigned     bool           `json:"leader_assigned"`
	DueAt              *time.Time     `json:"due_at,omitempty"`
	SourceQuote        string         `json:"source_quote"`
	Context            string         `json:"context"`
	ContextSnapshot    datatypes.JSON `json:"context_snapshot,omitempty"`
	Resolution         datatypes.JSON `json:"resolution,omitempty"`
}

func EncodeTodoEventSnapshot(todo *Todo) (datatypes.JSON, error) {
	if todo == nil || todo.ID == 0 {
		return nil, fmt.Errorf("todo event snapshot requires persisted todo")
	}
	encoded, err := json.Marshal(TodoEventSnapshot{
		Title: todo.Title, Target: todo.Target, ProjectID: todo.ProjectID,
		CommitmentStrength: todo.CommitmentStrength,
		LeaderAssigned:     todo.IsLeaderAssigned,
		DueAt:              todo.DueAt,
		SourceQuote:        todo.SourceQuote,
		Context:            todo.Context,
		ContextSnapshot:    todo.ContextSnapshot,
		Resolution:         todo.Resolution,
	})
	if err != nil {
		return nil, fmt.Errorf("encode todo event snapshot todo_id=%d: %w", todo.ID, err)
	}
	return datatypes.JSON(encoded), nil
}

// ExtractModels returns M3-owned support tables in dependency order.
func ExtractModels() []any {
	return []any{&TodoExtractWatermark{}, &TodoEvent{}, &ExtractionRun{}}
}
