package capture

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// ClueChatMode marks a clue channel so the chat poller skips it: a clue channel
// is not a Feishu conversation and has no messages to page through. M3 selects
// on related_group alone, so clues reach extraction like any other evidence.
const ClueChatMode = "clue"

const (
	clueSenderOpenID = "__clue__"
	clueSenderType   = "system"
	clueMessageType  = "clue"
	clueMessageSrc   = "clue"
	clueChatIDPrefix = "clue:"
)

// clueSourceIdentifier keeps a source usable as a stable channel key.
var clueSourceIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// ErrInvalidClue marks a malformed delivery so callers can separate a bad
// request from a storage failure.
var ErrInvalidClue = errors.New("invalid clue")

// ClueInput is one externally observed fact delivered into the evidence stream.
//
// M2 stores it verbatim and draws no conclusion from it: no classification, no
// severity, no "worth acting on" judgement, no follow-up fetching. A clue may
// be as thin as "a meeting ended"; working out what it means, what else to go
// read, and whether it deserves a Todo is M3's job.
type ClueInput struct {
	// Source names the clue producer and selects its channel, e.g. feishu_meeting.
	Source string
	// ExternalID is the producer-side identity used for idempotency. Redelivering
	// the same (source, external_id) is a no-op rather than a duplicate clue.
	ExternalID string
	Title      string
	Content    string
	// OccurredAt is when the fact happened. It is recorded in the clue body for
	// the model to read, not used as the stream position — see AppendClue.
	OccurredAt time.Time
}

// ClueResult reports where the clue landed and whether this call created it.
type ClueResult struct {
	ChatID    string `json:"chat_id"`
	MessageID string `json:"message_id"`
	Inserted  bool   `json:"inserted"`
}

// AppendClue delivers one clue into the evidence stream and wakes M3.
//
// Redelivery is safe: the composed message_id is unique, so a repeated clue
// returns inserted=false without touching the stored row or notifying M3.
func (s *Service) AppendClue(ctx context.Context, input ClueInput) (*ClueResult, error) {
	source := strings.TrimSpace(input.Source)
	if !clueSourceIdentifier.MatchString(source) {
		return nil, fmt.Errorf("%w: source %q must be a lowercase snake_case identifier of at most 32 chars", ErrInvalidClue, input.Source)
	}
	externalID := strings.TrimSpace(input.ExternalID)
	if externalID == "" {
		return nil, fmt.Errorf("%w: external_id must not be blank", ErrInvalidClue)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: title must not be blank", ErrInvalidClue)
	}
	if input.OccurredAt.IsZero() {
		return nil, fmt.Errorf("%w: occurred_at must be set", ErrInvalidClue)
	}
	chatID := clueChatIDPrefix + source
	messageID := chatID + ":" + externalID
	if len(messageID) > 64 {
		return nil, fmt.Errorf("%w: message id %q exceeds 64 chars; shorten source or external_id", ErrInvalidClue, messageID)
	}

	// create_time is when the clue entered the stream, not when the fact happened.
	// M3 advances its per-chat watermark by create_time, so back-dating a clue to
	// its subject's timestamp would drop it behind the watermark and it would
	// never be extracted. The fact's own time stays in the body for the model.
	lines := []string{title, "线索发生时间：" + input.OccurredAt.Format(time.RFC3339)}
	if body := strings.TrimSpace(input.Content); body != "" {
		lines = append(lines, body)
	}
	content := strings.Join(lines, "\n")
	createTime := s.now().UnixMilli()

	group, err := s.ensureClueChannel(ctx, chatID, source, createTime)
	if err != nil {
		return nil, err
	}
	message := &domain.Message{
		MessageID: messageID, ChatID: chatID, GroupID: &group.ID, ChatMode: ClueChatMode,
		SenderOpenID: clueSenderOpenID, SenderName: source, SenderType: clueSenderType,
		MessageType: clueMessageType, Content: content, CreateTime: createTime,
		Source: clueMessageSrc, RenderOK: true,
	}
	var inserted bool
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created, err := upsertMessage(tx, message)
		if err != nil {
			return err
		}
		inserted = created
		return nil
	}); err != nil {
		return nil, fmt.Errorf("persist clue %s: %w", messageID, err)
	}

	result := &ClueResult{ChatID: chatID, MessageID: messageID, Inserted: inserted}
	if !inserted || s.observer == nil {
		return result, nil
	}
	if err := s.observer.ChatScanned(ctx, ChatScanResult{
		ChatID: chatID, InsertedCount: 1, MessageIDs: []string{messageID},
		HighWater: createTime, LastMessageID: &messageID,
	}); err != nil {
		return nil, fmt.Errorf("notify pipeline for clue %s: %w", messageID, err)
	}
	return result, nil
}

// ensureClueChannel creates the channel on first delivery and keeps its activity
// timestamp fresh. related_group=1 is what puts the channel in M3's scan range.
func (s *Service) ensureClueChannel(ctx context.Context, chatID, source string, activeAt int64) (*domain.Group, error) {
	db := s.db.WithContext(ctx)
	var group domain.Group
	found := db.Where("chat_id = ?", chatID).Limit(1).Find(&group)
	if found.Error != nil {
		return nil, fmt.Errorf("load clue channel %s: %w", chatID, found.Error)
	}
	if found.RowsAffected == 0 {
		name := "线索：" + source
		group = domain.Group{
			ChatID: chatID, ChatMode: ClueChatMode, Name: &name,
			RelatedGroup: true, Tier: "hot", LastActiveAt: &activeAt,
		}
		if err := db.Create(&group).Error; err != nil {
			return nil, fmt.Errorf("create clue channel %s: %w", chatID, err)
		}
		return &group, nil
	}
	if group.ChatMode != ClueChatMode {
		return nil, fmt.Errorf("chat_id %s already exists with chat_mode=%q, refusing to reuse it as a clue channel", chatID, group.ChatMode)
	}
	if group.LastActiveAt == nil || *group.LastActiveAt < activeAt {
		if err := db.Model(&domain.Group{}).Where("id = ?", group.ID).
			Update("last_active_at", activeAt).Error; err != nil {
			return nil, fmt.Errorf("update clue channel %s activity: %w", chatID, err)
		}
	}
	return &group, nil
}
