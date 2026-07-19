package domain

import (
	"time"

	"gorm.io/datatypes"
)

// Message is the plaintext source-of-truth row captured from Feishu.
type Message struct {
	ID              uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	MessageID       string         `gorm:"column:message_id;type:varchar(64);not null;uniqueIndex:uk_message_id"`
	ChatID          string         `gorm:"column:chat_id;type:varchar(64);not null;index:idx_chat_create,priority:1"`
	GroupID         *uint64        `gorm:"column:group_id;type:bigint unsigned"`
	ChatMode        string         `gorm:"column:chat_mode;type:varchar(16);not null"`
	SenderOpenID    string         `gorm:"column:sender_open_id;type:varchar(64);not null;index:idx_sender"`
	SenderName      string         `gorm:"column:sender_name;type:varchar(256);not null"`
	SenderType      string         `gorm:"column:sender_type;type:varchar(16);not null"`
	MessageType     string         `gorm:"column:message_type;type:varchar(32);not null"`
	Content         string         `gorm:"column:content;type:mediumtext;not null"`
	ContentRaw      *string        `gorm:"column:content_raw;type:mediumtext"`
	MentionsJSON    datatypes.JSON `gorm:"column:mentions_json;type:json"`
	ReplyTo         *string        `gorm:"column:reply_to;type:varchar(64)"`
	RootID          *string        `gorm:"column:root_id;type:varchar(64)"`
	ThreadID        *string        `gorm:"column:thread_id;type:varchar(64);index:idx_thread"`
	CreateTime      int64          `gorm:"column:create_time;type:bigint;not null;index:idx_chat_create,priority:2;index:idx_mem0,priority:2"`
	UpdateTime      *int64         `gorm:"column:update_time;type:bigint"`
	Source          string         `gorm:"column:source;type:varchar(8);not null;default:poll"`
	RenderOK        bool           `gorm:"column:render_ok;type:tinyint(1);not null;default:1"`
	Mem0Processed   bool           `gorm:"column:mem0_processed;type:tinyint(1);not null;default:0;index:idx_mem0,priority:1"`
	Mem0ProcessedAt *time.Time     `gorm:"column:mem0_processed_at;type:datetime"`
	CreatedAt       time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`

	Group *Group `gorm:"foreignKey:GroupID;constraint:OnDelete:SET NULL"`
}

func (Message) TableName() string { return "message" }

// Checkpoint is the polling high-water state for one chat. Only the polling
// capture path may advance it.
type Checkpoint struct {
	ChatID              string     `gorm:"column:chat_id;type:varchar(64);primaryKey"`
	HighWaterCreateTime int64      `gorm:"column:high_water_create_time;type:bigint;not null"`
	LastMessageID       *string    `gorm:"column:last_message_id;type:varchar(64)"`
	BackfillDone        bool       `gorm:"column:backfill_done;type:tinyint(1);not null;default:1"`
	BackfillSince       int64      `gorm:"column:backfill_since;type:bigint;not null"`
	LastScanAt          *time.Time `gorm:"column:last_scan_at;type:datetime"`
	LastScanStatus      *string    `gorm:"column:last_scan_status;type:varchar(16)"`
	LastError           *string    `gorm:"column:last_error;type:text"`
	CreatedAt           time.Time  `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt           time.Time  `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (Checkpoint) TableName() string { return "chat_checkpoint" }

// CaptureModels returns M2-owned support tables in migration order.
func CaptureModels() []any {
	return []any{&Message{}, &Checkpoint{}}
}
