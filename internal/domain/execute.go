package domain

import (
	"time"

	"gorm.io/datatypes"
)

// ExecutionRun is M5's append-only record of one codex execution attempt for a
// Task. It captures what codex was asked to do, how long it took, whether it
// succeeded, and where any code change landed (branch/commit/diff). One Task can
// have multiple runs (retries), so this is not unique on task_id.
type ExecutionRun struct {
	ID         uint64 `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	TaskID     uint64 `gorm:"column:task_id;type:bigint unsigned;not null;index:idx_run_task"`
	ActionType string `gorm:"column:action_type;type:varchar(32);not null"`
	Stage      string `gorm:"column:stage;type:varchar(16);not null;default:execute"`
	// Sandbox is the codex sandbox level actually used: read-only for
	// investigate, workspace-write for code_change.
	Sandbox string `gorm:"column:sandbox;type:varchar(24);not null"`
	// Status: running -> succeeded | waiting | needs_human | failed.
	Status         string         `gorm:"column:status;type:varchar(16);not null;index:idx_run_status"`
	Prompt         string         `gorm:"column:prompt;type:mediumtext;not null"`
	CodexSessionID *string        `gorm:"column:codex_session_id;type:varchar(128)"`
	Summary        *string        `gorm:"column:summary;type:mediumtext"`
	Output         datatypes.JSON `gorm:"column:output;type:json"`
	// Effects is the agent's self-declared list of real-world side effects this
	// run produced (feishu message sent, doc created, meeting scheduled, MR
	// opened, permission requested, ...). It is a display-only, OPEN payload:
	// each element is a loose object with a free-form kind plus any extra fields
	// the agent chooses. Jarvis trusts these declarations verbatim and does NOT
	// verify them against lark-cli/git receipts. Unknown kinds and unknown fields
	// are stored and rendered as-is, never rejected.
	Effects        datatypes.JSON `gorm:"column:effects;type:json"`
	ErrorDetail    *string        `gorm:"column:error_detail;type:mediumtext"`
	// RepoPath/BaseBranch/Branch/Commit/DiffPath/MergeRequestURL are only set for
	// code_change runs. BaseBranch and Branch are persisted before a wait so the
	// resumed session can finish the same Git delivery path.
	RepoPath        *string `gorm:"column:repo_path;type:varchar(1024)"`
	BaseBranch      *string `gorm:"column:base_branch;type:varchar(256)"`
	Branch          *string `gorm:"column:branch;type:varchar(256)"`
	Commit          *string `gorm:"column:commit_sha;type:varchar(64)"`
	DiffPath        *string `gorm:"column:diff_path;type:varchar(1024)"`
	MergeRequestURL *string `gorm:"column:merge_request_url;type:varchar(1024)"`

	StartedAt  time.Time  `gorm:"column:started_at;type:datetime;not null"`
	FinishedAt *time.Time `gorm:"column:finished_at;type:datetime"`
	DurationMs *int64     `gorm:"column:duration_ms;type:bigint"`
	CreatedAt  time.Time  `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`

	Task *Task `gorm:"foreignKey:TaskID;constraint:OnDelete:RESTRICT"`
}

func (ExecutionRun) TableName() string { return "execution_run" }

// ExecuteModels returns the M5 execution audit tables for migration.
func ExecuteModels() []any {
	return []any{&ExecutionRun{}}
}
