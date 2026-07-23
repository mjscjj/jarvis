package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/datatypes"
)

// TodoExtractWatermark is M3's independent per-chat extraction cursor. M2's
// chat_checkpoint must never be reused for this purpose.
type TodoExtractWatermark struct {
	ChatID               string    `gorm:"column:chat_id;type:varchar(64);primaryKey"`
	LastScannedMessageID string    `gorm:"column:last_scanned_message_id;type:varchar(64);not null"`
	LastScannedAt        time.Time `gorm:"column:last_scanned_at;type:datetime;not null"`
	UpdatedAt            time.Time `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (TodoExtractWatermark) TableName() string { return "todo_extract_watermark" }

// TodoEvent is the append-only audit stream shared by M3 and later lifecycle
// owners. M3 writes actor=m3 and never moves a Todo beyond extracted.
type TodoEvent struct {
	ID         uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	TodoID     uint64         `gorm:"column:todo_id;type:bigint unsigned;not null;index:idx_todo_event_todo"`
	FromStatus *string        `gorm:"column:from_status;type:varchar(32)"`
	ToStatus   string         `gorm:"column:to_status;type:varchar(32);not null"`
	Actor      string         `gorm:"column:actor;type:varchar(16);not null"`
	Detail     datatypes.JSON `gorm:"column:detail;type:json"`
	Snapshot   datatypes.JSON `gorm:"column:snapshot;type:json"` // 事件发生时的不可变 Todo 语义快照；历史事件不回读当前 Todo 行
	CreatedAt  time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`

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
	return []any{&TodoExtractWatermark{}, &TodoEvent{}}
}
