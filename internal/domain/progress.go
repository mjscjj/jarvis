package domain

import (
	"time"

	"jarvis/internal/datatypes"
)

// TaskEvent is the append-only business state history for a Task. ExecutionRun
// remains the audit record for one Codex attempt; TaskEvent records what happened
// to the Task itself.
type TaskEvent struct {
	ID          uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	TaskID      uint64         `gorm:"column:task_id;not null;uniqueIndex:uk_task_event_version,priority:1;index:idx_task_event_time,priority:1"`
	TaskVersion int32          `gorm:"column:task_version;not null;uniqueIndex:uk_task_event_version,priority:2"`
	EventType   string         `gorm:"column:event_type;not null;index:idx_task_event_type"`
	FromStatus  *string        `gorm:"column:from_status"`
	ToStatus    string         `gorm:"column:to_status;not null"`
	ActorType   string         `gorm:"column:actor_type;not null"`
	ActorRef    *string        `gorm:"column:actor_ref"`
	RunID       *uint64        `gorm:"column:run_id;index:idx_task_event_run"`
	Detail      datatypes.JSON `gorm:"column:detail"`
	OccurredAt  time.Time      `gorm:"column:occurred_at;not null;index:idx_task_event_time,priority:2"`
	CreatedAt   time.Time      `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`

	Task *Task         `gorm:"foreignKey:TaskID;constraint:OnDelete:RESTRICT"`
	Run  *ExecutionRun `gorm:"foreignKey:RunID;constraint:OnDelete:RESTRICT"`
}

func (TaskEvent) TableName() string { return "task_event" }

// Fact is one append-only natural-language observation about some subject:
// what happened, when, and who noticed. Entity tables keep current structured
// state; Fact records the stream of things that happened to them.
//
// Facts are written as a side channel from wherever the system learns
// something — M3 while extracting, M5 while executing, background CRUD — and
// are read back two ways: as recent history for a subject (context snapshots)
// and as the evidence behind a day's digest.
type Fact struct {
	ID uint64 `gorm:"column:id;primaryKey;autoIncrement"`

	// SubjectType is deliberately not an enum. "project", "group" and "person"
	// are the types the system currently reads back, but a model that decides a
	// fact belongs to something else may write its own value rather than
	// discard the observation. Unknown types are stored, not rejected.
	SubjectType string `gorm:"column:subject_type;not null;index:idx_fact_subject_time,priority:1"`
	SubjectID   uint64 `gorm:"column:subject_id;not null;index:idx_fact_subject_time,priority:2"`

	// Description is the whole fact, in prose. There is no structured payload
	// beside it on purpose: the previous schema here carried an event_type enum
	// and had to be torn out. See migrateNaturalLanguageFacts.
	Description string `gorm:"column:description;not null"`

	// OccurredAt is when the fact happened, not when it was recorded, so a
	// backfilled fact lands on the right day. Callers select a natural day as a
	// half-open range over this column; there is no separate date column
	// because a stored local date would silently go wrong if the configured
	// timezone ever changed.
	OccurredAt time.Time `gorm:"column:occurred_at;not null;index:idx_fact_subject_time,priority:3;index:idx_fact_occurred_at"`

	// SourceKind and SourceID trace a fact back to what produced it (m3, m5,
	// task, run, background). Both optional: a fact is still useful when its
	// origin is a human poking the API.
	SourceKind *string `gorm:"column:source_kind"`
	SourceID   *uint64 `gorm:"column:source_id"`

	CreatedAt time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
}

func (Fact) TableName() string { return "fact" }

func ProgressModels() []any { return []any{&TaskEvent{}, &Fact{}} }
