package domain

import "time"

// AccessAuditEvent records access metadata only. Request and response bodies
// are intentionally excluded so the audit trail does not duplicate user data.
type AccessAuditEvent struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	OccurredAt    time.Time `gorm:"column:occurred_at;not null;index:idx_access_audit_time" json:"occurred_at"`
	ActorKind     string    `gorm:"column:actor_kind;not null;index:idx_access_audit_actor_time,priority:1" json:"actor_kind"`
	ActorID       string    `gorm:"column:actor_id;not null" json:"actor_id"`
	Operation     string    `gorm:"column:operation;not null;index:idx_access_audit_operation_time,priority:1" json:"operation"`
	Method        string    `gorm:"column:method;not null" json:"method"`
	Route         string    `gorm:"column:route;not null" json:"route"`
	ResourceType  string    `gorm:"column:resource_type;not null" json:"resource_type"`
	ResourceID    *string   `gorm:"column:resource_id" json:"resource_id"`
	StatusCode    int       `gorm:"column:status_code;not null" json:"status_code"`
	RequestID     string    `gorm:"column:request_id;not null;index:idx_access_audit_request" json:"request_id"`
	RemoteAddress string    `gorm:"column:remote_address;not null" json:"remote_address"`
}

func (AccessAuditEvent) TableName() string { return "access_audit_event" }

func SecurityModels() []any { return []any{&AccessAuditEvent{}} }
