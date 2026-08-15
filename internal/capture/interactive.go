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
// a direct P2P request or explicit group mention. Capture persists it before Task creation so
// the request and its recent conversation can be frozen into Task.background.
type InteractiveMessage struct {
	MessageID      string
	ChatID         string
	ChatMode       string
	ChatName       string
	SenderOpenID   string
	SenderName     string
	MessageType    string
	Content        string
	ContentRaw     string
	Mentions       json.RawMessage
	ParentID       string
	RootID         string
	ThreadID       string
	CreateTime     int64
	RecentMessages []InteractiveHistoryMessage
}

// InteractiveHistoryMessage is one source-rendered Feishu message immediately
// preceding an interactive request. CC Connect fetches at most 24 of these so
// the frozen conversation contains at most 25 messages including the request.
type InteractiveHistoryMessage struct {
	MessageID    string
	SenderOpenID string
	SenderName   string
	SenderType   string
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
	if err := s.captureInteractiveHistory(ctx, group, input); err != nil {
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
		if existing.ReplyTo == nil && strings.TrimSpace(input.ParentID) != "" {
			updates["reply_to"] = strings.TrimSpace(input.ParentID)
		}
		if existing.RootID == nil && strings.TrimSpace(input.RootID) != "" {
			updates["root_id"] = strings.TrimSpace(input.RootID)
		}
		if existing.ThreadID == nil && strings.TrimSpace(input.ThreadID) != "" {
			updates["thread_id"] = strings.TrimSpace(input.ThreadID)
		}
		if len(updates) > 0 {
			if err := s.db.WithContext(ctx).Model(&domain.Message{}).
				Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
				return nil, fmt.Errorf("mark interactive message_id=%s extraction skipped: %w", input.MessageID, err)
			}
		}
		existing.ExtractionSkipped = true
		existing.GroupID = &group.ID
		if value, ok := updates["reply_to"].(string); ok {
			existing.ReplyTo = &value
		}
		if value, ok := updates["root_id"].(string); ok {
			existing.RootID = &value
		}
		if value, ok := updates["thread_id"].(string); ok {
			existing.ThreadID = &value
		}
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

func (s *Service) captureInteractiveHistory(ctx context.Context, group *domain.Group, input InteractiveMessage) error {
	if len(input.RecentMessages) > 24 {
		return fmt.Errorf("interactive message recent history has %d messages, want at most 24", len(input.RecentMessages))
	}
	seen := map[string]struct{}{input.MessageID: {}}
	lastCreateTime := int64(0)
	for index := range input.RecentMessages {
		history := input.RecentMessages[index]
		history.MessageID = strings.TrimSpace(history.MessageID)
		history.SenderOpenID = strings.TrimSpace(history.SenderOpenID)
		history.SenderName = strings.TrimSpace(history.SenderName)
		history.SenderType = strings.TrimSpace(history.SenderType)
		history.MessageType = strings.TrimSpace(history.MessageType)
		history.Content = strings.TrimSpace(history.Content)
		if history.MessageID == "" || history.SenderOpenID == "" || history.MessageType == "" || history.Content == "" || history.CreateTime <= 0 {
			return fmt.Errorf("interactive history[%d] requires message_id, sender_open_id, message_type, content and positive create_time", index)
		}
		if history.CreateTime > input.CreateTime {
			return fmt.Errorf("interactive history message_id=%s is newer than anchor message_id=%s", history.MessageID, input.MessageID)
		}
		if history.CreateTime < lastCreateTime {
			return fmt.Errorf("interactive history must be ordered by create_time ascending")
		}
		lastCreateTime = history.CreateTime
		if _, exists := seen[history.MessageID]; exists {
			return fmt.Errorf("interactive history contains duplicate message_id=%s", history.MessageID)
		}
		seen[history.MessageID] = struct{}{}

		var existing domain.Message
		result := s.db.WithContext(ctx).Where("message_id = ?", history.MessageID).Limit(1).Find(&existing)
		if result.Error != nil {
			return fmt.Errorf("load interactive history message_id=%s: %w", history.MessageID, result.Error)
		}
		if result.RowsAffected == 1 {
			if existing.ChatID != input.ChatID {
				return fmt.Errorf("interactive history message_id=%s already belongs to chat_id=%s", history.MessageID, existing.ChatID)
			}
			if existing.GroupID != nil && *existing.GroupID != group.ID {
				return fmt.Errorf("interactive history message_id=%s group_id=%d conflicts with chat group_id=%d", history.MessageID, *existing.GroupID, group.ID)
			}
			updates := map[string]any{}
			if existing.GroupID == nil {
				updates["group_id"] = group.ID
			}
			if existing.ReplyTo == nil && strings.TrimSpace(history.ParentID) != "" {
				updates["reply_to"] = strings.TrimSpace(history.ParentID)
			}
			if existing.RootID == nil && strings.TrimSpace(history.RootID) != "" {
				updates["root_id"] = strings.TrimSpace(history.RootID)
			}
			if existing.ThreadID == nil && strings.TrimSpace(history.ThreadID) != "" {
				updates["thread_id"] = strings.TrimSpace(history.ThreadID)
			}
			if len(updates) > 0 {
				if err := s.db.WithContext(ctx).Model(&domain.Message{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
					return fmt.Errorf("enrich interactive history message_id=%s: %w", history.MessageID, err)
				}
			}
			continue
		}
		senderType := history.SenderType
		if senderType == "" {
			senderType = "unknown"
		}
		senderName := history.SenderName
		if senderName == "" {
			senderName = history.SenderOpenID
		}
		raw := history.ContentRaw
		mentions := datatypes.JSON(history.Mentions)
		if len(history.Mentions) == 0 {
			mentions = datatypes.JSON([]byte("[]"))
		}
		message := &domain.Message{
			MessageID: history.MessageID, ChatID: input.ChatID, GroupID: &group.ID, ChatMode: input.ChatMode,
			SenderOpenID: history.SenderOpenID, SenderName: senderName, SenderType: senderType,
			MessageType: history.MessageType, Content: history.Content, ContentRaw: &raw,
			MentionsJSON: mentions, ReplyTo: nullableString(history.ParentID), RootID: nullableString(history.RootID),
			ThreadID: nullableString(history.ThreadID), CreateTime: history.CreateTime,
			Source: "cc_connect_history", RenderOK: true, ExtractionSkipped: true,
		}
		if err := s.db.WithContext(ctx).Create(message).Error; err != nil {
			return fmt.Errorf("insert interactive history message_id=%s: %w", history.MessageID, err)
		}
	}
	return nil
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
