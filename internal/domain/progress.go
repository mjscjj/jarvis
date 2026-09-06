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

// PageRevision archives one entity summary page's previous text. Pages are
// lossy by design — compression drops detail on purpose, and this is the only
// record of what was dropped. It is deliberately not a Fact: a Fact answers
// "what happened in the world", a revision answers "our own note was edited",
// and mixing the two put stale page text back in front of the maintenance
// Agent as if it were evidence.
type PageRevision struct {
	ID uint64 `gorm:"column:id;primaryKey;autoIncrement"`

	PageType string `gorm:"column:page_type;not null;index:idx_page_revision_page,priority:1"`
	PageID   uint64 `gorm:"column:page_id;not null;index:idx_page_revision_page,priority:2"`

	// OldText is the whole previous page, never a truncated copy.
	OldText string `gorm:"column:old_text;not null"`

	ChangedAt time.Time `gorm:"column:changed_at;not null;index:idx_page_revision_page,priority:3"`
}

func (PageRevision) TableName() string { return "page_revision" }

// WorldProgress is Jarvis's latest evidence-backed assessment of one external
// subject during one period. It is not an objective fact and it is not the
// external product's official progress record. The owning adapter validates
// SubjectType/SubjectID on writes; string IDs keep the core independent from
// optional module tables.
type WorldProgress struct {
	ID            uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	SubjectType   string         `gorm:"column:subject_type;not null;size:64;uniqueIndex:uk_world_progress_subject_period,priority:1"`
	SubjectID     string         `gorm:"column:subject_id;not null;size:128;uniqueIndex:uk_world_progress_subject_period,priority:2"`
	PeriodKey     string         `gorm:"column:period_key;not null;size:64;uniqueIndex:uk_world_progress_subject_period,priority:3;index:idx_world_progress_period"`
	Signal        string         `gorm:"column:signal;not null;size:16;index:idx_world_progress_signal"`
	Summary       string         `gorm:"column:summary;not null;type:text"`
	Evidence      datatypes.JSON `gorm:"column:evidence;not null;type:text"`
	Version       int32          `gorm:"column:version;not null;default:0"`
	AssessedAt    time.Time      `gorm:"column:assessed_at;not null;index:idx_world_progress_assessed"`
	EvidenceUntil time.Time      `gorm:"column:evidence_until;not null"`
	CreatedAt     time.Time      `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (WorldProgress) TableName() string { return "world_progress" }

func ProgressModels() []any {
	return []any{&TaskEvent{}, &Fact{}, &PageRevision{}, &WorldProgress{}}
}
