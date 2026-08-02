package domain

import "time"

// ProactiveRun is the durable audit record for one proactive heartbeat Agent
// invocation. Input and Output keep the complete natural-language payloads;
// the remaining fields only describe the machine-owned run lifecycle.
type ProactiveRun struct {
	ID          uint64  `gorm:"column:id;primaryKey;autoIncrement"`
	TriggerType string  `gorm:"column:trigger_type;not null"`
	Engine      string  `gorm:"column:engine;not null"`
	Model       string  `gorm:"column:model;not null"`
	Status      string  `gorm:"column:status;not null;index:idx_proactive_run_status"`
	Input       string  `gorm:"column:input;not null"`
	Output      *string `gorm:"column:output"`
	ErrorDetail *string `gorm:"column:error_detail"`

	StartedAt  time.Time  `gorm:"column:started_at;not null;index:idx_proactive_run_started"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
	DurationMS *int64     `gorm:"column:duration_ms"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
}

func (ProactiveRun) TableName() string { return "proactive_run" }

func ProactiveModels() []any { return []any{&ProactiveRun{}} }
