package domain

import "time"

// CommentDelivery persists one external side effect independently of comment
// edits. The unique key also arbitrates concurrent delivery attempts.
type CommentDelivery struct {
	CommentID string    `gorm:"primaryKey"`
	Email     string    `gorm:"primaryKey"`
	Name      string    `gorm:"not null"`
	AppID     string    `gorm:"not null"`
	Payload   string    `gorm:"type:text;not null"`
	Status    string    `gorm:"not null"`
	MessageID string    `gorm:"not null;default:''"`
	Attempts  int       `gorm:"not null;default:0"`
	Error     string    `gorm:"not null;default:''"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (CommentDelivery) TableName() string { return "okr_workspace_comment_delivery" }
