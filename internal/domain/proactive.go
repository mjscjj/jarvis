package domain

import "time"

// ProactiveRun is the durable audit record for one proactive heartbeat Agent
// invocation. Input and Output keep the complete natural-language payloads;
// the remaining fields only describe the machine-owned run lifecycle.
type ProactiveRun struct {
	ID          uint64  `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	TriggerType string  `gorm:"column:trigger_type;type:varchar(16);not null"`
	Engine      string  `gorm:"column:engine;type:varchar(32);not null"`
	Model       string  `gorm:"column:model;type:varchar(128);not null"`
	Status      string  `gorm:"column:status;type:varchar(16);not null;index:idx_proactive_run_status"`
	Input       string  `gorm:"column:input;type:mediumtext;not null"`
	Output      *string `gorm:"column:output;type:mediumtext"`
	ErrorDetail *string `gorm:"column:error_detail;type:mediumtext"`

	StartedAt  time.Time  `gorm:"column:started_at;type:datetime;not null;index:idx_proactive_run_started"`
	FinishedAt *time.Time `gorm:"column:finished_at;type:datetime"`
	DurationMS *int64     `gorm:"column:duration_ms;type:bigint"`
	CreatedAt  time.Time  `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
}

func (ProactiveRun) TableName() string { return "proactive_run" }

func ProactiveModels() []any { return []any{&ProactiveRun{}} }
