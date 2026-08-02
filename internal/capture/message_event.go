package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jarvis/internal/datatypes"
)

const messageEventType = "im.message.receive_v1"

// MessageEvent is the flat NDJSON object emitted by
// `lark-cli event consume im.message.receive_v1`. The event transport renders
// content but preserves the platform identifiers used for exact idempotency.
type MessageEvent struct {
	Type        string         `json:"type"`
	EventID     string         `json:"event_id"`
	MessageID   string         `json:"message_id"`
	ChatID      string         `json:"chat_id"`
	ChatType    string         `json:"chat_type"`
	SenderID    string         `json:"sender_id"`
	SenderType  string         `json:"sender_type"`
	MessageType string         `json:"message_type"`
	Content     string         `json:"content"`
	CreateTime  string         `json:"create_time"`
	UpdateTime  string         `json:"update_time"`
	ReplyTo     string         `json:"reply_to"`
	RootID      string         `json:"root_id"`
	ThreadID    string         `json:"thread_id"`
	Mentions    []EventMention `json:"mentions"`
}

type EventMention struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// MessageEventResult reports the durable effect of one event delivery.
type MessageEventResult struct {
	ChatID    string
	MessageID string
	Inserted  bool
	Related   bool
}

// AppendMessageEvent persists one Feishu message event and wakes M3 only after
// a new message in an already-related conversation commits. It deliberately
// does not advance chat_checkpoint: the polling path remains the durable
// recovery cursor and later observes the same message_id as a duplicate.
func (s *Service) AppendMessageEvent(ctx context.Context, raw json.RawMessage) (*MessageEventResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("capture message event service is nil")
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, fmt.Errorf("capture message event payload is empty")
	}
	var event MessageEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("decode %s event: %w", messageEventType, err)
	}
	if err := validateMessageEvent(event); err != nil {
		return nil, err
	}
	createTime, _ := strconv.ParseInt(event.CreateTime, 10, 64)
	var updateTime *int64
	if event.UpdateTime != "" {
		parsed, _ := strconv.ParseInt(event.UpdateTime, 10, 64)
		updateTime = &parsed
	}
	mentions, err := json.Marshal(event.Mentions)
	if err != nil {
		return nil, fmt.Errorf("encode message event mentions message_id=%s: %w", event.MessageID, err)
	}
	rawText := string(raw)

	var (
		group    domain.Group
		inserted bool
	)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, err := ensureMessageEventGroup(tx, event.ChatID, event.ChatType, createTime)
		if err != nil {
			return err
		}
		group = *loaded
		message := &domain.Message{
			MessageID: event.MessageID, ChatID: event.ChatID, GroupID: &group.ID,
			ChatMode: group.ChatMode, SenderOpenID: event.SenderID,
			// im.message.receive_v1 carries no display name. Keep that absence
			// explicit instead of inventing one; M3 resolves known people by open_id.
			SenderName: "", SenderType: event.SenderType, MessageType: event.MessageType,
			Content: event.Content, ContentRaw: &rawText, MentionsJSON: datatypes.JSON(mentions),
			ReplyTo: nullableString(event.ReplyTo), RootID: nullableString(event.RootID),
			ThreadID: nullableString(event.ThreadID), CreateTime: createTime,
			UpdateTime: updateTime, Source: "event", RenderOK: knownMessageType(event.MessageType),
		}
		inserted, err = upsertMessage(tx, message)
		if err != nil {
			return err
		}
		if err := sinkResources(tx, message, extractResourceRefs(event.Content)); err != nil {
			return err
		}
		if group.LastActiveAt == nil || createTime > *group.LastActiveAt {
			if err := tx.Model(&domain.Group{}).Where("id = ?", group.ID).
				Update("last_active_at", createTime).Error; err != nil {
				return fmt.Errorf("update event chat activity chat_id=%s: %w", group.ChatID, err)
			}
			group.LastActiveAt = &createTime
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("persist message event message_id=%s: %w", event.MessageID, err)
	}

	result := &MessageEventResult{
		ChatID: event.ChatID, MessageID: event.MessageID,
		Inserted: inserted, Related: group.RelatedGroup,
	}
	if !inserted || !group.RelatedGroup || s.observer == nil {
		return result, nil
	}
	return result, s.observer.ChatScanned(ctx, ChatScanResult{
		ChatID: event.ChatID, InsertedCount: 1,
		MessageIDs: []string{event.MessageID}, HighWater: createTime,
		LastMessageID: &event.MessageID,
	})
}

func validateMessageEvent(event MessageEvent) error {
	if strings.TrimSpace(event.Type) != messageEventType {
		return fmt.Errorf("message event type=%q, want %s", event.Type, messageEventType)
	}
	for name, value := range map[string]string{
		"message_id": event.MessageID, "chat_id": event.ChatID,
		"sender_id": event.SenderID, "sender_type": event.SenderType,
		"message_type": event.MessageType, "create_time": event.CreateTime,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("message event %s is empty", name)
		}
	}
	if event.ChatType != "group" && event.ChatType != "p2p" {
		return fmt.Errorf("message event chat_type=%q is unsupported", event.ChatType)
	}
	createTime, err := strconv.ParseInt(event.CreateTime, 10, 64)
	if err != nil || createTime <= 0 {
		return fmt.Errorf("message event create_time=%q is not a positive millisecond timestamp", event.CreateTime)
	}
	if event.UpdateTime != "" {
		updateTime, err := strconv.ParseInt(event.UpdateTime, 10, 64)
		if err != nil || updateTime <= 0 {
			return fmt.Errorf("message event update_time=%q is not a positive millisecond timestamp", event.UpdateTime)
		}
	}
	return nil
}

func ensureMessageEventGroup(tx *gorm.DB, chatID, chatType string, createTime int64) (*domain.Group, error) {
	var group domain.Group
	result := tx.Where("chat_id = ?", chatID).Limit(1).Find(&group)
	if result.Error != nil {
		return nil, fmt.Errorf("load event chat chat_id=%s: %w", chatID, result.Error)
	}
	if result.RowsAffected > 0 {
		compatible := group.ChatMode == chatType || (chatType == "group" && group.ChatMode == "topic")
		if !compatible {
			return nil, fmt.Errorf("event chat_id=%s type=%q conflicts with discovered chat_mode=%q", chatID, chatType, group.ChatMode)
		}
		return &group, nil
	}

	// The event may beat the periodic discovery job. Create only the mechanical
	// skeleton available in the event; discovery later fills metadata and decides
	// whether a p2p belongs in the monitored Top-N. No history is backfilled.
	group = domain.Group{
		ChatID: chatID, ChatMode: chatType, RelatedGroup: false,
		Tier: "hot", LastActiveAt: &createTime,
	}
	if err := tx.Create(&group).Error; err != nil {
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, fmt.Errorf("create event chat chat_id=%s: %w", chatID, err)
		}
		if err := tx.Where("chat_id = ?", chatID).First(&group).Error; err != nil {
			return nil, fmt.Errorf("reload concurrently created event chat chat_id=%s: %w", chatID, err)
		}
	}
	checkpoint := domain.Checkpoint{
		ChatID: chatID, HighWaterCreateTime: createTime,
		BackfillDone: true, BackfillSince: createTime,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&checkpoint).Error; err != nil {
		return nil, fmt.Errorf("initialize event chat checkpoint chat_id=%s: %w", chatID, err)
	}
	return &group, nil
}
