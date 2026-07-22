package domain

import (
	"time"

	"gorm.io/datatypes"
)

// TaskEvent is the append-only business state history for a Task. ExecutionRun
// remains the audit record for one Codex attempt; TaskEvent records what happened
// to the Task itself.
type TaskEvent struct {
	ID          uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	TaskID      uint64         `gorm:"column:task_id;type:bigint unsigned;not null;uniqueIndex:uk_task_event_version,priority:1;index:idx_task_event_time,priority:1"`
	TaskVersion int32          `gorm:"column:task_version;not null;uniqueIndex:uk_task_event_version,priority:2"`
	EventType   string         `gorm:"column:event_type;type:varchar(32);not null;index:idx_task_event_type"`
	FromStatus  *string        `gorm:"column:from_status;type:varchar(24)"`
	ToStatus    string         `gorm:"column:to_status;type:varchar(24);not null"`
	ActorType   string         `gorm:"column:actor_type;type:varchar(16);not null"`
	ActorRef    *string        `gorm:"column:actor_ref;type:varchar(128)"`
	RunID       *uint64        `gorm:"column:run_id;type:bigint unsigned;index:idx_task_event_run"`
	Detail      datatypes.JSON `gorm:"column:detail;type:json"`
	OccurredAt  time.Time      `gorm:"column:occurred_at;type:datetime;not null;index:idx_task_event_time,priority:2"`
	CreatedAt   time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`

	Task *Task         `gorm:"foreignKey:TaskID;constraint:OnDelete:RESTRICT"`
	Run  *ExecutionRun `gorm:"foreignKey:RunID;constraint:OnDelete:RESTRICT"`
}

func (TaskEvent) TableName() string { return "task_event" }

// ProjectEvent is the append-only history of project state, progress,
// milestones, decisions and blockers. Project keeps the current state.
type ProjectEvent struct {
	ID         uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	ProjectID  uint64         `gorm:"column:project_id;type:bigint unsigned;not null;index:idx_project_event_time,priority:1"`
	EventType  string         `gorm:"column:event_type;type:varchar(32);not null;index:idx_project_event_type"`
	Title      string         `gorm:"column:title;type:varchar(512);not null"`
	Summary    *string        `gorm:"column:summary;type:text"`
	FromStatus *string        `gorm:"column:from_status;type:varchar(24)"`
	ToStatus   *string        `gorm:"column:to_status;type:varchar(24)"`
	ActorType  string         `gorm:"column:actor_type;type:varchar(16);not null"`
	ActorRef   *string        `gorm:"column:actor_ref;type:varchar(128)"`
	SourceType *string        `gorm:"column:source_type;type:varchar(32)"`
	SourceID   *string        `gorm:"column:source_id;type:varchar(128)"`
	Detail     datatypes.JSON `gorm:"column:detail;type:json"`
	EventKey   string         `gorm:"column:event_key;type:char(64);not null;uniqueIndex:uk_project_event_key"`
	OccurredAt time.Time      `gorm:"column:occurred_at;type:datetime;not null;index:idx_project_event_time,priority:2"`
	CreatedAt  time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`

	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:RESTRICT"`
}

func (ProjectEvent) TableName() string { return "project_event" }

func ProgressModels() []any { return []any{&TaskEvent{}, &ProjectEvent{}} }
