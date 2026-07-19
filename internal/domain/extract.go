package domain

import (
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

// TodoEvent is the append-only audit stream shared by M3 and later lifecycle
// owners. M3 writes actor=m3 and never moves a Todo beyond extracted.
type TodoEvent struct {
	ID         uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	TodoID     uint64         `gorm:"column:todo_id;type:bigint unsigned;not null;index:idx_todo_event_todo"`
	FromStatus *string        `gorm:"column:from_status;type:varchar(32)"`
	ToStatus   string         `gorm:"column:to_status;type:varchar(32);not null"`
	Actor      string         `gorm:"column:actor;type:varchar(16);not null"`
	Detail     datatypes.JSON `gorm:"column:detail;type:json"`
	CreatedAt  time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`

	Todo *Todo `gorm:"foreignKey:TodoID;constraint:OnDelete:CASCADE"`
}

func (TodoEvent) TableName() string { return "todo_event" }

// ExtractModels returns M3-owned support tables in dependency order.
func ExtractModels() []any {
	return []any{&TodoExtractWatermark{}, &TodoEvent{}}
}
