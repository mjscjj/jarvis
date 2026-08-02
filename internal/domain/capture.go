package domain

import (
	"time"

	"jarvis/internal/datatypes"
)

// Message is the plaintext source-of-truth row captured from Feishu.
type Message struct {
	ID           uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	MessageID    string         `gorm:"column:message_id;not null;uniqueIndex:uk_message_id"`
	ChatID       string         `gorm:"column:chat_id;not null;index:idx_chat_create,priority:1"`
	GroupID      *uint64        `gorm:"column:group_id"`
	ChatMode     string         `gorm:"column:chat_mode;not null"`
	SenderOpenID string         `gorm:"column:sender_open_id;not null;index:idx_sender"`
	SenderName   string         `gorm:"column:sender_name;not null"`
	SenderType   string         `gorm:"column:sender_type;not null"`
	MessageType  string         `gorm:"column:message_type;not null"`
	Content      string         `gorm:"column:content;not null"`
	ContentRaw   *string        `gorm:"column:content_raw"`
	MentionsJSON datatypes.JSON `gorm:"column:mentions_json"`
	ReplyTo      *string        `gorm:"column:reply_to"`
	RootID       *string        `gorm:"column:root_id"`
	ThreadID     *string        `gorm:"column:thread_id;index:idx_thread"`
	CreateTime   int64          `gorm:"column:create_time;not null;index:idx_chat_create,priority:2"`
	UpdateTime   *int64         `gorm:"column:update_time"`
	Source       string         `gorm:"column:source;not null;default:poll"`
	RenderOK     bool           `gorm:"column:render_ok;not null;default:1"`
	CreatedAt    time.Time      `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`

	Group *Group `gorm:"foreignKey:GroupID;constraint:OnDelete:SET NULL"`
}

func (Message) TableName() string { return "message" }

// Checkpoint is the polling high-water state for one chat. Only the polling
// capture path may advance it.
type Checkpoint struct {
	ChatID              string     `gorm:"column:chat_id;primaryKey"`
	HighWaterCreateTime int64      `gorm:"column:high_water_create_time;not null"`
	LastMessageID       *string    `gorm:"column:last_message_id"`
	BackfillDone        bool       `gorm:"column:backfill_done;not null;default:1"`
	BackfillSince       int64      `gorm:"column:backfill_since;not null"`
	LastScanAt          *time.Time `gorm:"column:last_scan_at"`
	LastScanStatus      *string    `gorm:"column:last_scan_status"`
	LastError           *string    `gorm:"column:last_error"`
	CreatedAt           time.Time  `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt           time.Time  `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (Checkpoint) TableName() string { return "chat_checkpoint" }

// PrincipalActivityCheckpoint is the durable cursor for discovering group
// chats where the principal has spoken. It is separate from chat_checkpoint:
// this cursor tracks one cross-chat search, while chat_checkpoint tracks
// per-conversation message capture.
type PrincipalActivityCheckpoint struct {
	PrincipalOpenID string    `gorm:"column:principal_open_id;primaryKey"`
	LastSearchAt    int64     `gorm:"column:last_search_at;not null"`
	UpdatedAt       time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (PrincipalActivityCheckpoint) TableName() string {
	return "principal_activity_checkpoint"
}

// CaptureModels returns M2-owned support tables in migration order.
func CaptureModels() []any {
	return []any{&Message{}, &Checkpoint{}, &PrincipalActivityCheckpoint{}}
}
