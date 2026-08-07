package domain

import (
	"time"

	"jarvis/internal/datatypes"
)

// ExecutionRun is M5's append-only record of one codex execution attempt for a
// Task. It captures what codex was asked to do, how long it took, and whether it
// succeeded. One Task can have multiple runs (retries), so this is not unique on
// task_id.
//
// What the run produced in the outside world — including where a code change
// landed — lives in Effects, declared by the agent itself. There are no
// branch/commit/MR columns: how code gets delivered is the agent's judgment, not
// a shape this table imposes.
type ExecutionRun struct {
	ID         uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	TaskID     uint64 `gorm:"column:task_id;not null;index:idx_run_task"`
	ActionType string `gorm:"column:action_type;not null"`
	Stage      string `gorm:"column:stage;not null;default:execute"`
	// Sandbox is the Agent sandbox level actually used for this run. It is
	// recorded for audit and is not derived from action_type.
	Sandbox string `gorm:"column:sandbox;not null"`
	// Status: running -> succeeded | waiting | needs_human | failed.
	Status         string         `gorm:"column:status;not null;index:idx_run_status"`
	Prompt         string         `gorm:"column:prompt;not null"`
	CodexSessionID *string        `gorm:"column:codex_session_id"`
	Summary        *string        `gorm:"column:summary"`
	Output         datatypes.JSON `gorm:"column:output"`
	// Effects is the agent's self-declared list of real-world side effects this
	// run produced (feishu message sent, doc created, meeting scheduled, MR
	// opened, permission requested, ...). It is a display-only, OPEN payload:
	// each element is a loose object with a free-form kind plus any extra fields
	// the agent chooses. Jarvis trusts these declarations verbatim and does NOT
	// verify them against lark-cli/git receipts. Unknown kinds and unknown fields
	// are stored and rendered as-is, never rejected.
	Effects     datatypes.JSON `gorm:"column:effects"`
	ErrorDetail *string        `gorm:"column:error_detail"`
	// Token fields are nullable so runs created before usage persistence can be
	// distinguished from a real, reported zero-token run.
	InputTokens           *int64 `gorm:"column:input_tokens"`
	CachedInputTokens     *int64 `gorm:"column:cached_input_tokens"`
	OutputTokens          *int64 `gorm:"column:output_tokens"`
	ReasoningOutputTokens *int64 `gorm:"column:reasoning_output_tokens"`
	// RepoPath is the working copy this run was pointed at, when one resolved. It
	// records where the agent worked, not how it delivered.
	RepoPath *string `gorm:"column:repo_path"`

	StartedAt  time.Time  `gorm:"column:started_at;not null;index:idx_execution_run_started"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
	DurationMs *int64     `gorm:"column:duration_ms"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`

	Task *Task `gorm:"foreignKey:TaskID;constraint:OnDelete:RESTRICT"`
}

func (ExecutionRun) TableName() string { return "execution_run" }

// ExecuteModels returns the M5 execution audit tables for migration.
func ExecuteModels() []any {
	return []any{&ExecutionRun{}}
}
