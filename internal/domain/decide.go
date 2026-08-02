package domain

import (
	"time"

	"gorm.io/datatypes"
)

// DecisionAudit is the decision stage's append-only record for routing and
// confirmation. Scoring columns (confidence/risk and their factor breakdowns,
// plus the threshold config version) belonged to the retired rule engine and
// were removed: the model returns a disposition, not a score.
type DecisionAudit struct {
	ID             uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	TodoID         uint64         `gorm:"column:todo_id;type:bigint unsigned;not null;index:idx_audit_todo"`
	TaskID         *uint64        `gorm:"column:task_id;type:bigint unsigned;index:idx_audit_task"`
	TS             time.Time      `gorm:"column:ts;type:datetime;not null;index:idx_audit_ts"`
	Route          string         `gorm:"column:route;type:varchar(16);not null"`
	RouteReason    string         `gorm:"column:route_reason;type:varchar(64);not null"`
	MatchedRules   datatypes.JSON `gorm:"column:matched_rules;type:json"`
	DecisionEngine string         `gorm:"column:decision_engine;type:varchar(16)"`
	CodexSessionID *string        `gorm:"column:codex_session_id;type:varchar(128)"`
	Approver       string         `gorm:"column:approver;type:varchar(16)"`
	Channel        string         `gorm:"column:channel;type:varchar(16)"`
	EventID        *string        `gorm:"column:event_id;type:varchar(64)"`
	IdempotencyKey *string        `gorm:"column:idempotency_key;type:varchar(64)"`
	FinalStatus    string         `gorm:"column:final_status;type:varchar(16)"`
}

func (DecisionAudit) TableName() string { return "decision_audit" }

func DecideModels() []any {
	return []any{&DecisionAudit{}}
}
