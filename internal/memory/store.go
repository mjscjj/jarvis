package memory

import (
	"context"
	"fmt"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// PendingMessage is the minimal joined projection required by the memory job.
type PendingMessage struct {
	ID         uint64
	MessageID  string
	ChatID     string
	ChatName   string
	ProjectID  *uint64
	SenderName string
	SenderType string
	Content    string
	CreateTime int64
	RenderOK   bool
}

type messageStore interface {
	ListPending(context.Context, int) ([]PendingMessage, error)
	MarkProcessed(context.Context, []uint64, time.Time) error
}

// GORMStore persists memory-processing state in the same MySQL source of truth
// as captured messages.
type GORMStore struct {
	db *gorm.DB
}

func NewGORMStore(db *gorm.DB) (*GORMStore, error) {
	if db == nil {
		return nil, fmt.Errorf("memory store db is nil")
	}
	return &GORMStore{db: db}, nil
}

func (s *GORMStore) ListPending(ctx context.Context, limit int) ([]PendingMessage, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("memory pending limit must be positive")
	}
	var messages []PendingMessage
	err := s.db.WithContext(ctx).
		Table("message AS m").
		Select(`m.id, m.message_id, m.chat_id, COALESCE(g.name, '') AS chat_name,
			g.project_id, m.sender_name, m.sender_type, m.content, m.create_time, m.render_ok`).
		Joins("JOIN feishu_group AS g ON g.chat_id = m.chat_id").
		Where("m.mem0_processed = ? AND g.related_group = ? AND g.include_in_memory = ?", false, true, true).
		Order("m.chat_id ASC, m.create_time ASC, m.id ASC").
		Limit(limit).
		Scan(&messages).Error
	if err != nil {
		return nil, fmt.Errorf("list messages pending memory: %w", err)
	}
	return messages, nil
}

func (s *GORMStore) MarkProcessed(ctx context.Context, ids []uint64, processedAt time.Time) error {
	if len(ids) == 0 {
		return fmt.Errorf("memory processed message IDs are empty")
	}
	result := s.db.WithContext(ctx).Model(&domain.Message{}).
		Where("id IN ? AND mem0_processed = ?", ids, false).
		Updates(map[string]any{
			"mem0_processed":    true,
			"mem0_processed_at": processedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("mark messages memory processed: %w", result.Error)
	}
	if result.RowsAffected != int64(len(ids)) {
		return fmt.Errorf("mark messages memory processed affected=%d, want %d", result.RowsAffected, len(ids))
	}
	return nil
}
