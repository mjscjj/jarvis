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

// InteractiveMessage is the exact transport envelope CC Connect received for
// an explicit interactive request. Capture persists it before Task creation so
// the request and its recent conversation can be frozen into Task.background.
type InteractiveMessage struct {
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

// CaptureInteractive records an explicit direct-to-M5 request without waking
// M3. extraction_skipped is a durable routing fact: the message remains normal
// conversation evidence, but cannot materialize a second Task later.
func (s *Service) CaptureInteractive(ctx context.Context, input InteractiveMessage) (*domain.Message, error) {
	input.MessageID = strings.TrimSpace(input.MessageID)
	input.ChatID = strings.TrimSpace(input.ChatID)
	input.ChatMode = strings.TrimSpace(input.ChatMode)
	input.ChatName = strings.TrimSpace(input.ChatName)
	input.SenderOpenID = strings.TrimSpace(input.SenderOpenID)
	input.SenderName = strings.TrimSpace(input.SenderName)
	input.MessageType = strings.TrimSpace(input.MessageType)
	input.Content = strings.TrimSpace(input.Content)
	if input.MessageID == "" || input.ChatID == "" || input.SenderOpenID == "" || input.MessageType == "" || input.Content == "" || input.CreateTime <= 0 {
		return nil, fmt.Errorf("interactive message requires message_id, chat_id, sender_open_id, message_type, content and positive create_time")
	}
	switch input.ChatMode {
	case "group", "p2p", "topic":
	default:
		return nil, fmt.Errorf("interactive message chat_mode=%q is unsupported", input.ChatMode)
	}

	group, err := s.ensureInteractiveGroup(ctx, input)
	if err != nil {
		return nil, err
	}
	var existing domain.Message
	result := s.db.WithContext(ctx).Where("message_id = ?", input.MessageID).Limit(1).Find(&existing)
	if result.Error != nil {
		return nil, fmt.Errorf("load interactive message_id=%s: %w", input.MessageID, result.Error)
	}
	if result.RowsAffected == 1 {
		if existing.ChatID != input.ChatID {
			return nil, fmt.Errorf("interactive message_id=%s already belongs to chat_id=%s", input.MessageID, existing.ChatID)
		}
		if existing.GroupID != nil && *existing.GroupID != group.ID {
			return nil, fmt.Errorf("interactive message_id=%s group_id=%d conflicts with chat group_id=%d", input.MessageID, *existing.GroupID, group.ID)
		}
		updates := map[string]any{}
		if !existing.ExtractionSkipped {
			updates["extraction_skipped"] = true
		}
		if existing.GroupID == nil {
			updates["group_id"] = group.ID
		}
		if len(updates) > 0 {
			if err := s.db.WithContext(ctx).Model(&domain.Message{}).
				Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
				return nil, fmt.Errorf("mark interactive message_id=%s extraction skipped: %w", input.MessageID, err)
			}
		}
		existing.ExtractionSkipped = true
		existing.GroupID = &group.ID
		return &existing, nil
	}

	raw := input.ContentRaw
	message := &domain.Message{
		MessageID: input.MessageID, ChatID: input.ChatID, GroupID: &group.ID, ChatMode: input.ChatMode,
		SenderOpenID: input.SenderOpenID, SenderName: input.SenderName, SenderType: "user",
		MessageType: input.MessageType, Content: input.Content, ContentRaw: &raw,
		MentionsJSON: datatypes.JSON(input.Mentions), ReplyTo: nullableString(input.ParentID),
		RootID: nullableString(input.RootID), ThreadID: nullableString(input.ThreadID),
		CreateTime: input.CreateTime, Source: "cc_connect", RenderOK: true, ExtractionSkipped: true,
	}
	if len(input.Mentions) == 0 {
		message.MentionsJSON = datatypes.JSON([]byte("[]"))
	}
	if err := s.db.WithContext(ctx).Create(message).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, fmt.Errorf("interactive message_id=%s raced with another delivery: %w", input.MessageID, err)
		}
		return nil, fmt.Errorf("insert interactive message_id=%s: %w", input.MessageID, err)
	}
	return message, nil
}

func (s *Service) ensureInteractiveGroup(ctx context.Context, input InteractiveMessage) (*domain.Group, error) {
	var group domain.Group
	result := s.db.WithContext(ctx).Where("chat_id = ?", input.ChatID).Limit(1).Find(&group)
	if result.Error != nil {
		return nil, fmt.Errorf("load interactive chat_id=%s: %w", input.ChatID, result.Error)
	}
	if result.RowsAffected == 0 {
		group = domain.Group{
			ChatID: input.ChatID, ChatMode: input.ChatMode, RelatedGroup: true,
			Tier: "hot", IncludeInMemory: true, LastActiveAt: &input.CreateTime,
		}
		if input.ChatName != "" {
			group.Name = &input.ChatName
		}
		if err := s.db.WithContext(ctx).Create(&group).Error; err != nil {
			return nil, fmt.Errorf("create interactive chat_id=%s: %w", input.ChatID, err)
		}
		return &group, nil
	}
	if group.ChatMode != input.ChatMode && !(group.ChatMode == "topic" && input.ChatMode == "group") {
		return nil, fmt.Errorf("interactive chat_id=%s mode=%s conflicts with captured mode=%s", input.ChatID, input.ChatMode, group.ChatMode)
	}
	updates := map[string]any{"related_group": true, "last_active_at": input.CreateTime}
	if group.Name == nil && input.ChatName != "" {
		updates["name"] = input.ChatName
		group.Name = &input.ChatName
	}
	if err := s.db.WithContext(ctx).Model(&domain.Group{}).Where("id = ?", group.ID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("refresh interactive chat_id=%s: %w", input.ChatID, err)
	}
	group.RelatedGroup = true
	group.LastActiveAt = &input.CreateTime
	return &group, nil
}
