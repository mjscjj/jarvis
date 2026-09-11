package domain

import (
	"time"

	"jarvis/internal/datatypes"
)

// ChatSession is Jarvis's durable interactive conversation. Native Agent
// thread IDs are adapter details and never become the product session ID.
type ChatSession struct {
	ID              string         `gorm:"column:id;primaryKey"`
	Title           string         `gorm:"column:title;not null;index:idx_chat_session_title"`
	Agent           string         `gorm:"column:agent;not null"`
	Model           string         `gorm:"column:model;not null"`
	ReasoningEffort string         `gorm:"column:reasoning_effort;not null;default:medium"`
	NativeThreadID  *string        `gorm:"column:native_thread_id"`
	Sources         datatypes.JSON `gorm:"column:sources"`
	Draft           datatypes.JSON `gorm:"column:draft"`
	ArchivedAt      *time.Time     `gorm:"column:archived_at;index:idx_chat_session_archived_updated,priority:1"`
	CreatedAt       time.Time      `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt       time.Time      `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime;index:idx_chat_session_archived_updated,priority:2"`
}

func (ChatSession) TableName() string { return "chat_session" }

// ChatMessage keeps the complete visible conversation. Meta remains open JSON
// for attachments, page/source snapshots and provider-specific diagnostics.
type ChatMessage struct {
	ID        string         `gorm:"column:id;primaryKey"`
	SessionID string         `gorm:"column:session_id;not null;index:idx_chat_message_session_created,priority:1"`
	Role      string         `gorm:"column:role;not null"`
	Text      string         `gorm:"column:text;not null"`
	Agent     *string        `gorm:"column:agent"`
	Model     *string        `gorm:"column:model"`
	Meta      datatypes.JSON `gorm:"column:meta"`
	CreatedAt time.Time      `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime;index:idx_chat_message_session_created,priority:2"`

	Session *ChatSession `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
}

func (ChatMessage) TableName() string { return "chat_message" }

// ChatAttachment points at a file copied into Jarvis's local chat data root.
type ChatAttachment struct {
	ID        string    `gorm:"column:id;primaryKey"`
	SessionID string    `gorm:"column:session_id;not null;index:idx_chat_attachment_session"`
	MessageID *string   `gorm:"column:message_id;index:idx_chat_attachment_message"`
	Name      string    `gorm:"column:name;not null"`
	MIMEType  string    `gorm:"column:mime_type;not null"`
	SizeBytes int64     `gorm:"column:size_bytes;not null"`
	LocalPath string    `gorm:"column:local_path;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`

	Session *ChatSession `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
}

func (ChatAttachment) TableName() string { return "chat_attachment" }

func ChatModels() []any { return []any{&ChatSession{}, &ChatMessage{}, &ChatAttachment{}} }
