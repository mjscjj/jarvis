// Package toolquery exposes small read-only projections of locally captured
// messages and resources for jarvis-tools. Lists stay compact; callers load one
// captured resource explicitly when they need its local path or extracted text.
package toolquery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var (
	ErrInvalidInput = errors.New("invalid tool query input")
	ErrNotFound     = errors.New("captured resource not found")
)

const maxLimit = 100

type MessageFilter struct {
	ChatID       string
	SenderOpenID string
	Keyword      string
	From         *time.Time
	Until        *time.Time
	Limit        int
}

type MessageView struct {
	ID           uint64  `json:"id"`
	MessageID    string  `json:"message_id"`
	ChatID       string  `json:"chat_id"`
	GroupID      *uint64 `json:"group_id"`
	SenderOpenID string  `json:"sender_open_id"`
	SenderName   string  `json:"sender_name"`
	MessageType  string  `json:"message_type"`
	Content      string  `json:"content"`
	ReplyTo      *string `json:"reply_to"`
	RootID       *string `json:"root_id"`
	ThreadID     *string `json:"thread_id"`
	CreateTime   int64   `json:"create_time"`
	Source       string  `json:"source"`
}

type ResourceFilter struct {
	ChatID       string
	MessageID    string
	ResourceType string
	Keyword      string
	Limit        int
}

type ResourceSummary struct {
	ID              uint64    `json:"id"`
	ResourceType    string    `json:"resource_type"`
	Name            *string   `json:"name"`
	URL             *string   `json:"url"`
	MIMEType        *string   `json:"mime_type"`
	SizeBytes       *int64    `json:"size_bytes"`
	SourceMessageID *string   `json:"source_message_id"`
	GroupID         *uint64   `json:"group_id"`
	Downloaded      bool      `json:"downloaded"`
	CreatedAt       time.Time `json:"created_at"`
}

type ResourceView struct {
	ResourceSummary
	FileKey       *string   `json:"file_key"`
	MinuteToken   *string   `json:"minute_token"`
	DocToken      *string   `json:"doc_token"`
	LocalPath     *string   `json:"local_path"`
	ContentHash   *string   `json:"content_hash"`
	ExtractedText *string   `json:"extracted_text"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("tool query db is nil")
	}
	return &Service{db: db}, nil
}

func (s *Service) ListMessages(ctx context.Context, filter MessageFilter) ([]MessageView, error) {
	if err := validateLimit(filter.Limit); err != nil {
		return nil, err
	}
	if filter.From != nil && filter.Until != nil && !filter.From.Before(*filter.Until) {
		return nil, fmt.Errorf("%w: from must be before until", ErrInvalidInput)
	}
	query := s.db.WithContext(ctx).Model(&domain.Message{})
	if value := strings.TrimSpace(filter.ChatID); value != "" {
		query = query.Where("chat_id = ?", value)
	}
	if value := strings.TrimSpace(filter.SenderOpenID); value != "" {
		query = query.Where("sender_open_id = ?", value)
	}
	if value := strings.TrimSpace(filter.Keyword); value != "" {
		query = query.Where("content LIKE ?", "%"+value+"%")
	}
	if filter.From != nil {
		query = query.Where("create_time >= ?", filter.From.UnixMilli())
	}
	if filter.Until != nil {
		query = query.Where("create_time < ?", filter.Until.UnixMilli())
	}
	var rows []domain.Message
	if err := query.Order("create_time DESC, id DESC").Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list captured messages: %w", err)
	}
	items := make([]MessageView, len(rows))
	for i := range rows {
		items[i] = messageView(&rows[i])
	}
	return items, nil
}

func (s *Service) ListResources(ctx context.Context, filter ResourceFilter) ([]ResourceSummary, error) {
	if err := validateLimit(filter.Limit); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&domain.Resource{})
	if value := strings.TrimSpace(filter.ChatID); value != "" {
		query = query.Joins("JOIN message ON message.message_id = resource.source_message_id").
			Where("message.chat_id = ?", value)
	}
	if value := strings.TrimSpace(filter.MessageID); value != "" {
		query = query.Where("resource.source_message_id = ?", value)
	}
	if value := strings.TrimSpace(filter.ResourceType); value != "" {
		query = query.Where("resource.resource_type = ?", value)
	}
	if value := strings.TrimSpace(filter.Keyword); value != "" {
		like := "%" + value + "%"
		query = query.Where("resource.name LIKE ? OR resource.url LIKE ? OR resource.extracted_text LIKE ?", like, like, like)
	}
	var rows []domain.Resource
	if err := query.Order("resource.created_at DESC, resource.id DESC").Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list captured resources: %w", err)
	}
	items := make([]ResourceSummary, len(rows))
	for i := range rows {
		items[i] = resourceSummary(&rows[i])
	}
	return items, nil
}

func (s *Service) GetResource(ctx context.Context, id uint64) (*ResourceView, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: resource id must be positive", ErrInvalidInput)
	}
	var row domain.Resource
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get captured resource id=%d: %w", id, err)
	}
	return &ResourceView{
		ResourceSummary: resourceSummary(&row), FileKey: row.FileKey,
		MinuteToken: row.MinuteToken, DocToken: row.DocToken, LocalPath: row.LocalPath,
		ContentHash: row.ContentHash, ExtractedText: row.ExtractedText, UpdatedAt: row.UpdatedAt,
	}, nil
}

func validateLimit(limit int) error {
	if limit < 1 || limit > maxLimit {
		return fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, maxLimit)
	}
	return nil
}

func messageView(row *domain.Message) MessageView {
	return MessageView{
		ID: row.ID, MessageID: row.MessageID, ChatID: row.ChatID, GroupID: row.GroupID,
		SenderOpenID: row.SenderOpenID, SenderName: row.SenderName,
		MessageType: row.MessageType, Content: row.Content, ReplyTo: row.ReplyTo,
		RootID: row.RootID, ThreadID: row.ThreadID, CreateTime: row.CreateTime, Source: row.Source,
	}
}

func resourceSummary(row *domain.Resource) ResourceSummary {
	return ResourceSummary{
		ID: row.ID, ResourceType: row.ResourceType, Name: row.Name, URL: row.URL,
		MIMEType: row.MIMEType, SizeBytes: row.SizeBytes, SourceMessageID: row.SourceMessageID,
		GroupID: row.GroupID, Downloaded: row.Downloaded, CreatedAt: row.CreatedAt,
	}
}
