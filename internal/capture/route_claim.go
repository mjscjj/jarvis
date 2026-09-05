package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var ErrInvalidRouteClaim = errors.New("invalid message route claim")

// RouteClaimMessage is the exact Feishu message envelope that an interactive
// transport has accepted for its own execution path. The claim is a durable
// routing fact only: it must not create a Task, wake M3, or change which chats
// M2 monitors.
type RouteClaimMessage struct {
	MessageID    string
	ChatID       string
	ChatMode     string
	ChatName     string
	SenderOpenID string
	SenderName   string
	MessageType  string
	Content      string
	ContentRaw   string
	Mentions     json.RawMessage
	ParentID     string
	RootID       string
	ThreadID     string
	CreateTime   int64
}

// CaptureRouteClaim persists one current message and excludes that exact row
// from M3 admission. Redelivery is idempotent by Feishu message_id.
func (s *Service) CaptureRouteClaim(ctx context.Context, input RouteClaimMessage) (*domain.Message, error) {
	input.MessageID = strings.TrimSpace(input.MessageID)
	input.ChatID = strings.TrimSpace(input.ChatID)
	input.ChatMode = strings.TrimSpace(input.ChatMode)
	input.ChatName = strings.TrimSpace(input.ChatName)
	input.SenderOpenID = strings.TrimSpace(input.SenderOpenID)
	input.SenderName = strings.TrimSpace(input.SenderName)
	input.MessageType = strings.TrimSpace(input.MessageType)
	input.Content = strings.TrimSpace(input.Content)
	input.ParentID = strings.TrimSpace(input.ParentID)
	input.RootID = strings.TrimSpace(input.RootID)
	input.ThreadID = strings.TrimSpace(input.ThreadID)
	if input.MessageID == "" || input.ChatID == "" || input.SenderOpenID == "" || input.MessageType == "" || input.Content == "" || input.CreateTime <= 0 {
		return nil, fmt.Errorf("%w: message_id, chat_id, sender_open_id, message_type, content and positive create_time are required", ErrInvalidRouteClaim)
	}
	switch input.ChatMode {
	case "group", "p2p", "topic":
	default:
		return nil, fmt.Errorf("%w: chat_mode=%q is unsupported", ErrInvalidRouteClaim, input.ChatMode)
	}

	group, err := s.ensureRouteClaimGroup(ctx, input)
	if err != nil {
		return nil, err
	}
	var existing domain.Message
	result := s.db.WithContext(ctx).Where("message_id = ?", input.MessageID).Limit(1).Find(&existing)
	if result.Error != nil {
		return nil, fmt.Errorf("load route claim message_id=%s: %w", input.MessageID, result.Error)
	}
	if result.RowsAffected == 1 {
		if existing.ChatID != input.ChatID {
			return nil, fmt.Errorf("%w: message_id=%s already belongs to chat_id=%s", ErrInvalidRouteClaim, input.MessageID, existing.ChatID)
		}
		if existing.GroupID != nil && *existing.GroupID != group.ID {
			return nil, fmt.Errorf("%w: message_id=%s group_id=%d conflicts with chat group_id=%d", ErrInvalidRouteClaim, input.MessageID, *existing.GroupID, group.ID)
		}
		updates := map[string]any{}
		if !existing.ExtractionSkipped {
			updates["extraction_skipped"] = true
		}
		if existing.GroupID == nil {
			updates["group_id"] = group.ID
		}
		if existing.ContentRaw == nil && strings.TrimSpace(input.ContentRaw) != "" {
			updates["content_raw"] = input.ContentRaw
		}
		if len(existing.MentionsJSON) == 0 && len(input.Mentions) > 0 {
			updates["mentions_json"] = datatypes.JSON(input.Mentions)
		}
		if existing.ReplyTo == nil && input.ParentID != "" {
			updates["reply_to"] = input.ParentID
		}
		if existing.RootID == nil && input.RootID != "" {
			updates["root_id"] = input.RootID
		}
		if existing.ThreadID == nil && input.ThreadID != "" {
			updates["thread_id"] = input.ThreadID
		}
		if len(updates) > 0 {
			if err := s.db.WithContext(ctx).Model(&domain.Message{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
				return nil, fmt.Errorf("persist route claim message_id=%s: %w", input.MessageID, err)
			}
			if err := s.db.WithContext(ctx).Where("id = ?", existing.ID).Take(&existing).Error; err != nil {
				return nil, fmt.Errorf("reload route claim message_id=%s: %w", input.MessageID, err)
			}
		}
		return &existing, nil
	}

	raw := input.ContentRaw
	mentions := datatypes.JSON(input.Mentions)
	if len(input.Mentions) == 0 {
		mentions = datatypes.JSON([]byte("[]"))
	}
	message := &domain.Message{
		MessageID: input.MessageID, ChatID: input.ChatID, GroupID: &group.ID, ChatMode: input.ChatMode,
		SenderOpenID: input.SenderOpenID, SenderName: input.SenderName, SenderType: "user",
		MessageType: input.MessageType, Content: input.Content, ContentRaw: &raw, MentionsJSON: mentions,
		ReplyTo: nullableString(input.ParentID), RootID: nullableString(input.RootID), ThreadID: nullableString(input.ThreadID),
		CreateTime: input.CreateTime, Source: "cc_connect", RenderOK: true, ExtractionSkipped: true,
	}
	if err := s.db.WithContext(ctx).Create(message).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, fmt.Errorf("route claim message_id=%s raced with another delivery: %w", input.MessageID, err)
		}
		return nil, fmt.Errorf("insert route claim message_id=%s: %w", input.MessageID, err)
	}
	return message, nil
}

func (s *Service) ensureRouteClaimGroup(ctx context.Context, input RouteClaimMessage) (*domain.Group, error) {
	var group domain.Group
	result := s.db.WithContext(ctx).Where("chat_id = ?", input.ChatID).Limit(1).Find(&group)
	if result.Error != nil {
		return nil, fmt.Errorf("load route claim chat_id=%s: %w", input.ChatID, result.Error)
	}
	if result.RowsAffected == 0 {
		group = domain.Group{
			ChatID: input.ChatID, ChatMode: input.ChatMode, Tier: "cold",
			RelatedGroup: false, IncludeInMemory: true, LastActiveAt: &input.CreateTime,
		}
		if input.ChatName != "" {
			group.Name = &input.ChatName
		}
		if err := s.db.WithContext(ctx).Create(&group).Error; err != nil {
			return nil, fmt.Errorf("create route claim chat_id=%s: %w", input.ChatID, err)
		}
		return &group, nil
	}
	if group.ChatMode != input.ChatMode && !(group.ChatMode == "topic" && input.ChatMode == "group") {
		return nil, fmt.Errorf("%w: chat_id=%s mode=%s conflicts with captured mode=%s", ErrInvalidRouteClaim, input.ChatID, input.ChatMode, group.ChatMode)
	}
	updates := map[string]any{}
	if group.Name == nil && input.ChatName != "" {
		updates["name"] = input.ChatName
		group.Name = &input.ChatName
	}
	if group.LastActiveAt == nil || *group.LastActiveAt < input.CreateTime {
		updates["last_active_at"] = input.CreateTime
		group.LastActiveAt = &input.CreateTime
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&domain.Group{}).Where("id = ?", group.ID).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("refresh route claim chat_id=%s: %w", input.ChatID, err)
		}
	}
	return &group, nil
}
