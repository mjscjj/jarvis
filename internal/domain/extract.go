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

// Observation is something worth remembering that asks nothing of the
// principal: a decision reached in a group, a fact someone stated, work another
// person owns, a constraint discovered while executing.
//
// It exists because a Todo was previously the only way anything could enter the
// pipeline, so "worth knowing" was forced to become "worth doing" (an
// action_type=notify_principal Todo) and then flowed through decision and
// execution as if it were work. Observations are terminal by design: they are
// stored, surfaced in digests and project context, and never routed, never
// materialized into a Task.
type Observation struct {
	ID uint64 `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	// Producer is the stage that saw it: m3 (from messages) or m5 (from
	// executing). Kept because the two have different evidence shapes.
	Producer string `gorm:"column:producer;type:varchar(16);not null;index:idx_observation_producer"`
	// Subject is free text naming what this is about, used as the retrieval
	// anchor. The model writes it; Go never parses it.
	Subject string `gorm:"column:subject;type:varchar(512);not null"`
	Content string `gorm:"column:content;type:text;not null"`

	ProjectID *uint64 `gorm:"column:project_id;type:bigint unsigned;index:idx_observation_project"`
	GroupID   *uint64 `gorm:"column:group_id;type:bigint unsigned;index:idx_observation_group"`
	// SourceRunID links an execution-time observation back to the run that found
	// it; nil for M3 observations.
	SourceRunID *uint64 `gorm:"column:source_run_id;type:bigint unsigned;index:idx_observation_run"`

	SourceMessageIDs datatypes.JSON `gorm:"column:source_message_ids;type:json"`
	SourceQuote      string         `gorm:"column:source_quote;type:text"`
	// Payload keeps whatever else the producer attached (M5 enrichment kind and
	// label, for instance) without widening this struct per producer.
	Payload datatypes.JSON `gorm:"column:payload;type:json"`

	// DedupKey makes re-extraction idempotent. One message can yield several
	// distinct observations, so the key hashes the content too, not just origin.
	DedupKey string `gorm:"column:dedup_key;type:char(64);not null;uniqueIndex:uk_observation_dedup"`

	// ObservedAt is when the fact happened; CreatedAt is when it landed. A
	// backfilled observation has an old ObservedAt and a fresh CreatedAt, so
	// incremental readers must page on CreatedAt/ID.
	ObservedAt time.Time `gorm:"column:observed_at;type:datetime;not null;index:idx_observation_observed"`
	CreatedAt  time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
}

func (Observation) TableName() string { return "observation" }

// Producers of an observation. M3 sees messages; M5 sees the real world while
// executing a Task.
const (
	ObservationProducerM3 = "m3"
	ObservationProducerM5 = "m5"
)

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
	return []any{&TodoExtractWatermark{}, &TodoEvent{}, &Observation{}}
}
