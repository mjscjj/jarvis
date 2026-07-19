package capture

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/larkcli"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const cliTimeLayout = "2006-01-02 15:04"

type runner interface {
	Run(ctx context.Context, out any, args ...string) error
}

// Options contains capture policy already decided by the technical design.
type Options struct {
	PageSize    int
	ScanWorkers int
	HotAge      time.Duration
	WarmAge     time.Duration
	Location    *time.Location
}

// Service owns conversation discovery and polling state transitions.
type Service struct {
	db   *gorm.DB
	lark runner
	opts Options
	now  func() time.Time
}

func NewService(db *gorm.DB, lark runner, opts Options) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("capture db is nil")
	}
	if lark == nil {
		return nil, fmt.Errorf("capture lark-cli runner is nil")
	}
	if opts.PageSize < 1 || opts.PageSize > 50 {
		return nil, fmt.Errorf("capture page size must be between 1 and 50")
	}
	if opts.ScanWorkers <= 0 {
		return nil, fmt.Errorf("capture scan workers must be positive")
	}
	if opts.HotAge <= 0 || opts.WarmAge <= opts.HotAge {
		return nil, fmt.Errorf("capture tier ages must satisfy 0 < hot < warm")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("capture location is nil")
	}
	return &Service{db: db, lark: lark, opts: opts, now: time.Now}, nil
}

// ReplaceRelatedGroups atomically replaces the capture allowlist. Every chat
// must already be discovered and must be a group/topic conversation. The list
// is runtime data, not a compiled-in or configured fixed-size allowlist.
func (s *Service) ReplaceRelatedGroups(chatIDs []string) error {
	chatIDs, err := normalizeChatIDs(chatIDs)
	if err != nil {
		return err
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		var groups []domain.Group
		if len(chatIDs) > 0 {
			if err := tx.Select("chat_id", "chat_mode", "external").Where("chat_id IN ?", chatIDs).Find(&groups).Error; err != nil {
				return fmt.Errorf("load related group candidates: %w", err)
			}
		}
		if len(groups) != len(chatIDs) {
			found := make(map[string]struct{}, len(groups))
			for _, group := range groups {
				found[group.ChatID] = struct{}{}
			}
			missing := make([]string, 0)
			for _, chatID := range chatIDs {
				if _, ok := found[chatID]; !ok {
					missing = append(missing, chatID)
				}
			}
			return fmt.Errorf("related groups are not discovered: %s", strings.Join(missing, ","))
		}
		for _, group := range groups {
			switch group.ChatMode {
			case "group", "topic":
			case "p2p":
				// 私聊可手动加入名单，但仅限内部同事；外部私聊不监听。
				if group.External {
					return fmt.Errorf("related chat_id=%s is an external p2p and cannot be monitored", group.ChatID)
				}
			default:
				return fmt.Errorf("related chat_id=%s has unsupported chat_mode=%q", group.ChatID, group.ChatMode)
			}
		}

		if err := tx.Model(&domain.Group{}).Where("related_group = ?", true).Update("related_group", false).Error; err != nil {
			return fmt.Errorf("clear related groups: %w", err)
		}
		if len(chatIDs) == 0 {
			return nil
		}
		result := tx.Model(&domain.Group{}).Where("chat_id IN ?", chatIDs).Update("related_group", true)
		if result.Error != nil {
			return fmt.Errorf("set related groups: %w", result.Error)
		}
		if result.RowsAffected != int64(len(chatIDs)) {
			return fmt.Errorf("set related groups affected=%d, want %d", result.RowsAffected, len(chatIDs))
		}
		return nil
	})
}

// OpenInternalP2P marks every already-discovered internal p2p chat as related,
// so existing 私聊 join monitoring in one pass. Discovery already auto-opens
// new p2p; this covers the backlog captured before that behavior existed.
// It never touches groups/topics and skips external p2p. Returns how many
// chats were newly opened.
func (s *Service) OpenInternalP2P() (int64, error) {
	result := s.db.Model(&domain.Group{}).
		Where("chat_mode = ? AND external = ? AND related_group = ?", "p2p", false, false).
		Update("related_group", true)
	if result.Error != nil {
		return 0, fmt.Errorf("open internal p2p chats: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func normalizeChatIDs(chatIDs []string) ([]string, error) {
	normalized := make([]string, 0, len(chatIDs))
	seen := make(map[string]struct{}, len(chatIDs))
	for _, chatID := range chatIDs {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" {
			return nil, fmt.Errorf("related chat_id is empty")
		}
		if _, ok := seen[chatID]; ok {
			return nil, fmt.Errorf("related chat_id is duplicated: %s", chatID)
		}
		seen[chatID] = struct{}{}
		normalized = append(normalized, chatID)
	}
	return normalized, nil
}

// DiscoverChats enumerates every user-visible chat. New chats start at now and
// therefore never backfill history.
func (s *Service) DiscoverChats(ctx context.Context) (err error) {
	record, err := s.beginScan("discover", nil, nil, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if finishErr := s.finishScanError(record, err); finishErr != nil {
				err = errors.Join(err, finishErr)
			}
		}
	}()

	pageToken := ""
	for {
		var response ChatListResponse
		args := []string{
			"im", "+chat-list", "--as", "user", "--types", "p2p,group",
			"--sort", "active_time", "--page-size", "100",
		}
		if pageToken != "" {
			args = append(args, "--page-token", pageToken)
		}
		if err = s.lark.Run(ctx, &response, args...); err != nil {
			return fmt.Errorf("list chats page=%d: %w", record.PageCount+1, err)
		}
		if err = s.persistDiscoveredChats(response.Data.Chats); err != nil {
			return fmt.Errorf("persist discovered chats page=%d: %w", record.PageCount+1, err)
		}
		record.FetchedCount += int32(len(response.Data.Chats))
		record.PageCount++
		if !response.Data.HasMore {
			break
		}
		if response.Data.PageToken == "" {
			return fmt.Errorf("chat list page=%d has_more=true with empty page_token", record.PageCount)
		}
		pageToken = response.Data.PageToken
	}
	if err = s.recomputeTiers(); err != nil {
		return err
	}
	return s.finishScanOK(record, nil)
}

func (s *Service) persistDiscoveredChats(chats []CLIChat) error {
	nowMS := s.now().UnixMilli()
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, chat := range chats {
			if chat.ChatID == "" {
				return fmt.Errorf("chat_id is empty")
			}
			if chat.ChatMode != "group" && chat.ChatMode != "p2p" && chat.ChatMode != "topic" {
				return fmt.Errorf("chat %s has unsupported chat_mode %q", chat.ChatID, chat.ChatMode)
			}
			// 内部 p2p 私聊自动纳入监听：只要是内部同事的私聊（external=false），
			// 发现时即置 related_group=1，新私聊也自动纳入，无需手动加名单。
			// 外部私聊与普通群/话题群不在此自动开启（群仍走手动名单）。
			autoRelated := chat.ChatMode == "p2p" && !chat.External
			group := domain.Group{
				ChatID:       chat.ChatID,
				ChatMode:     chat.ChatMode,
				Name:         nullableString(chat.Name),
				Description:  nullableString(chat.Description),
				OwnerOpenID:  nullableString(chat.OwnerID),
				External:     chat.External,
				TenantKey:    nullableString(chat.TenantKey),
				RelatedGroup: autoRelated,
				Tier:         "cold",
			}
			// 更新列：p2p 私聊连带 related_group 一起 upsert（存量私聊也会被开启）；
			// 非 p2p 不动 related_group，避免覆盖用户对普通群的手动名单设置。
			updateColumns := []string{
				"chat_mode", "name", "description", "owner_open_id", "external", "tenant_key", "updated_at",
			}
			if chat.ChatMode == "p2p" {
				updateColumns = append(updateColumns, "related_group")
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "chat_id"}},
				DoUpdates: clause.AssignmentColumns(updateColumns),
			}).Create(&group).Error; err != nil {
				return fmt.Errorf("upsert group chat_id=%s: %w", chat.ChatID, err)
			}

			checkpoint := domain.Checkpoint{
				ChatID:              chat.ChatID,
				HighWaterCreateTime: nowMS,
				BackfillDone:        true,
				BackfillSince:       nowMS,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&checkpoint).Error; err != nil {
				return fmt.Errorf("initialize checkpoint chat_id=%s: %w", chat.ChatID, err)
			}
		}
		return nil
	})
}

func (s *Service) recomputeTiers() error {
	now := s.now()
	hotCutoff := now.Add(-s.opts.HotAge).UnixMilli()
	warmCutoff := now.Add(-s.opts.WarmAge).UnixMilli()
	result := s.db.Exec(`
		UPDATE feishu_group
		SET tier = CASE
			WHEN pinned = 1 THEN 'hot'
			WHEN last_active_at IS NULL THEN 'cold'
			WHEN last_active_at >= ? THEN 'hot'
			WHEN last_active_at >= ? THEN 'warm'
			ELSE 'cold'
		END
	`, hotCutoff, warmCutoff)
	if result.Error != nil {
		return fmt.Errorf("recompute chat tiers: %w", result.Error)
	}
	return nil
}

// ScanChatNow is the "just marked related, scan immediately" entry point. It
// guarantees the chat has a sane scan window before delegating to ScanChat: a
// group discovered long ago still carries its original discovery high-water, so
// this resets a stale window to now, then does an incremental scan. It never
// backfills history (window start = now for a fresh related group).
func (s *Service) ScanChatNow(ctx context.Context, chatID string) error {
	if chatID == "" {
		return fmt.Errorf("scan chat_id is empty")
	}
	if err := s.ensureScanWindow(chatID); err != nil {
		return err
	}
	return s.ScanChat(ctx, chatID)
}

// ensureScanWindow moves the high-water forward to now when a chat has never
// captured a message (last_active_at is NULL). This keeps the first scan of a
// newly related group cheap (only messages from now on) and avoids replaying
// the discovery-time window that may lie far in the past.
func (s *Service) ensureScanWindow(chatID string) error {
	var group domain.Group
	if err := s.db.Select("id", "last_active_at").Where("chat_id = ?", chatID).First(&group).Error; err != nil {
		return fmt.Errorf("load group chat_id=%s: %w", chatID, err)
	}
	if group.LastActiveAt != nil {
		return nil
	}
	nowMS := s.now().UnixMilli()
	if err := s.db.Model(&domain.Checkpoint{}).
		Where("chat_id = ? AND high_water_create_time < ?", chatID, nowMS).
		Update("high_water_create_time", nowMS).Error; err != nil {
		return fmt.Errorf("initialize scan window chat_id=%s: %w", chatID, err)
	}
	return nil
}

// ScanChat incrementally captures one previously discovered chat.
func (s *Service) ScanChat(ctx context.Context, chatID string) (err error) {
	if chatID == "" {
		return fmt.Errorf("scan chat_id is empty")
	}
	var group domain.Group
	if err := s.db.Where("chat_id = ?", chatID).First(&group).Error; err != nil {
		return fmt.Errorf("load group chat_id=%s: %w", chatID, err)
	}
	if !group.RelatedGroup {
		return fmt.Errorf("chat_id=%s is not a related group", chatID)
	}
	var checkpoint domain.Checkpoint
	if err := s.db.First(&checkpoint, "chat_id = ?", chatID).Error; err != nil {
		return fmt.Errorf("load checkpoint chat_id=%s: %w", chatID, err)
	}

	scanType := "scan_" + group.Tier
	windowStart := checkpoint.HighWaterCreateTime
	record, err := s.beginScan(scanType, &group.ID, &chatID, &windowStart)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if finishErr := s.finishChatError(record, &checkpoint, err); finishErr != nil {
				err = errors.Join(err, finishErr)
			}
		}
	}()

	pageToken := ""
	currentHW := windowStart
	lastMessageID := checkpoint.LastMessageID
	for {
		var response MessageListResponse
		args := []string{
			"im", "+chat-messages-list", "--as", "user", "--chat-id", chatID,
			"--start", time.UnixMilli(windowStart).In(s.opts.Location).Format(time.RFC3339),
			"--order", "asc", "--page-size", strconv.Itoa(s.opts.PageSize), "--no-reactions",
		}
		if pageToken != "" {
			args = append(args, "--page-token", pageToken)
		}
		if err = s.lark.Run(ctx, &response, args...); err != nil {
			return fmt.Errorf("list messages chat_id=%s page=%d: %w", chatID, record.PageCount+1, err)
		}
		messages := flattenMessages(response.Data.Messages)
		var inserted int32
		inserted, currentHW, lastMessageID, err = s.persistMessagePage(&group, messages, currentHW, lastMessageID)
		if err != nil {
			return fmt.Errorf("persist messages chat_id=%s page=%d: %w", chatID, record.PageCount+1, err)
		}
		record.FetchedCount += int32(len(messages))
		record.InsertedCount += inserted
		record.PageCount++
		if !response.Data.HasMore {
			break
		}
		if response.Data.PageToken == "" {
			return fmt.Errorf("message list chat_id=%s page=%d has_more=true with empty page_token", chatID, record.PageCount)
		}
		pageToken = response.Data.PageToken
	}

	if err = s.finishChatOK(record, &checkpoint, currentHW, lastMessageID); err != nil {
		return err
	}
	return s.recomputeGroupTier(group.ID)
}

// ScanRelated scans every related chat in one pass. Tier no longer gates
// scheduling: all related chats share the single scan cadence. Chat failures
// are collected and returned after the other chats finish; no failed chat
// advances past its last committed page.
func (s *Service) ScanRelated(ctx context.Context) error {
	var groups []domain.Group
	if err := s.db.Select("id", "chat_id").Where("related_group = ?", true).Order("id ASC").Find(&groups).Error; err != nil {
		return fmt.Errorf("list related chats: %w", err)
	}
	return s.scanGroups(ctx, groups)
}

// scanGroups runs ScanChat over the given groups with the worker pool. Errors
// are collected and joined; a single chat failure never aborts the others.
func (s *Service) scanGroups(ctx context.Context, groups []domain.Group) error {
	jobs := make(chan string)
	errorsCh := make(chan error, len(groups)+1)
	var workers sync.WaitGroup
	for range s.opts.ScanWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for chatID := range jobs {
				if err := s.ScanChat(ctx, chatID); err != nil {
					errorsCh <- err
				}
			}
		}()
	}

	for _, group := range groups {
		select {
		case jobs <- group.ChatID:
		case <-ctx.Done():
			errorsCh <- ctx.Err()
			close(jobs)
			workers.Wait()
			close(errorsCh)
			return joinErrors(errorsCh)
		}
	}
	close(jobs)
	workers.Wait()
	close(errorsCh)
	return joinErrors(errorsCh)
}

func joinErrors(errorsCh <-chan error) error {
	errs := make([]error, 0)
	for err := range errorsCh {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Service) recomputeGroupTier(groupID uint64) error {
	now := s.now()
	result := s.db.Exec(`
		UPDATE feishu_group
		SET tier = CASE
			WHEN pinned = 1 THEN 'hot'
			WHEN last_active_at IS NULL THEN 'cold'
			WHEN last_active_at >= ? THEN 'hot'
			WHEN last_active_at >= ? THEN 'warm'
			ELSE 'cold'
		END
		WHERE id = ?
	`, now.Add(-s.opts.HotAge).UnixMilli(), now.Add(-s.opts.WarmAge).UnixMilli(), groupID)
	if result.Error != nil {
		return fmt.Errorf("recompute chat tier group_id=%d: %w", groupID, result.Error)
	}
	return nil
}

func (s *Service) persistMessagePage(group *domain.Group, messages []CLIMessage, currentHW int64, lastMessageID *string) (int32, int64, *string, error) {
	var inserted int32
	err := s.db.Transaction(func(tx *gorm.DB) error {
		for _, item := range messages {
			message, err := s.toDomainMessage(group, item)
			if err != nil {
				return err
			}
			created, err := upsertMessage(tx, message)
			if err != nil {
				return err
			}
			if created {
				inserted++
			}
			if err := sinkResources(tx, message, extractResourceRefs(item.Content)); err != nil {
				return err
			}
			if message.CreateTime > currentHW || (message.CreateTime == currentHW && (lastMessageID == nil || message.MessageID > *lastMessageID)) {
				currentHW = message.CreateTime
				id := message.MessageID
				lastMessageID = &id
			}
		}
		if len(messages) == 0 {
			return nil
		}
		if err := tx.Model(&domain.Checkpoint{}).
			Where("chat_id = ? AND high_water_create_time <= ?", group.ChatID, currentHW).
			Updates(map[string]any{
				"high_water_create_time": currentHW,
				"last_message_id":        lastMessageID,
			}).Error; err != nil {
			return fmt.Errorf("advance checkpoint chat_id=%s: %w", group.ChatID, err)
		}
		if group.LastActiveAt == nil || currentHW > *group.LastActiveAt {
			if err := tx.Model(&domain.Group{}).Where("id = ?", group.ID).Update("last_active_at", currentHW).Error; err != nil {
				return fmt.Errorf("update group activity chat_id=%s: %w", group.ChatID, err)
			}
			group.LastActiveAt = &currentHW
		}
		return nil
	})
	return inserted, currentHW, lastMessageID, err
}

func (s *Service) toDomainMessage(group *domain.Group, item CLIMessage) (*domain.Message, error) {
	if item.MessageID == "" {
		return nil, fmt.Errorf("chat_id=%s contains message with empty message_id", group.ChatID)
	}
	createTime, err := parseCLITime(item.CreateTime, s.opts.Location)
	if err != nil {
		return nil, fmt.Errorf("parse message %s create_time %q: %w", item.MessageID, item.CreateTime, err)
	}
	var updateTime *int64
	if item.UpdateTime != "" {
		parsed, err := parseCLITime(item.UpdateTime, s.opts.Location)
		if err != nil {
			return nil, fmt.Errorf("parse message %s update_time %q: %w", item.MessageID, item.UpdateTime, err)
		}
		updateTime = &parsed
	}
	senderID := item.Sender.ID
	if item.Sender.OpenBotID != "" {
		senderID = item.Sender.OpenBotID
	}
	if senderID == "" {
		return nil, fmt.Errorf("message %s sender id is empty", item.MessageID)
	}
	return &domain.Message{
		MessageID:     item.MessageID,
		ChatID:        group.ChatID,
		GroupID:       &group.ID,
		ChatMode:      group.ChatMode,
		SenderOpenID:  senderID,
		SenderName:    item.Sender.Name,
		SenderType:    item.Sender.SenderType,
		MessageType:   item.MessageType,
		Content:       item.Content,
		ReplyTo:       nullableString(item.ParentID),
		RootID:        nullableString(item.RootID),
		ThreadID:      nullableString(item.ThreadID),
		CreateTime:    createTime,
		UpdateTime:    updateTime,
		Source:        "poll",
		RenderOK:      knownMessageType(item.MessageType),
		Mem0Processed: false,
	}, nil
}

func upsertMessage(tx *gorm.DB, incoming *domain.Message) (bool, error) {
	var existing domain.Message
	result := tx.Where("message_id = ?", incoming.MessageID).Limit(1).Find(&existing)
	if result.Error != nil {
		return false, fmt.Errorf("load message %s: %w", incoming.MessageID, result.Error)
	}
	if result.RowsAffected == 0 {
		if err := tx.Create(incoming).Error; err != nil {
			return false, fmt.Errorf("insert message %s: %w", incoming.MessageID, err)
		}
		return true, nil
	}
	if incoming.UpdateTime == nil || (existing.UpdateTime != nil && *incoming.UpdateTime <= *existing.UpdateTime) {
		return false, nil
	}
	updates := map[string]any{
		"content":           incoming.Content,
		"content_raw":       incoming.ContentRaw,
		"message_type":      incoming.MessageType,
		"sender_open_id":    incoming.SenderOpenID,
		"sender_name":       incoming.SenderName,
		"sender_type":       incoming.SenderType,
		"update_time":       incoming.UpdateTime,
		"render_ok":         incoming.RenderOK,
		"mem0_processed":    false,
		"mem0_processed_at": nil,
	}
	if err := tx.Model(&existing).Updates(updates).Error; err != nil {
		return false, fmt.Errorf("update edited message %s: %w", incoming.MessageID, err)
	}
	return false, nil
}

func sinkResources(tx *gorm.DB, message *domain.Message, refs []resourceRef) error {
	for _, ref := range refs {
		fileKey := ref.FileKey
		resource := domain.Resource{
			ResourceType:    ref.ResourceType,
			FileKey:         &fileKey,
			MinuteToken:     ref.MinuteToken,
			DocToken:        ref.DocToken,
			URL:             ref.URL,
			SourceMessageID: &message.MessageID,
			GroupID:         message.GroupID,
			Downloaded:      false,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_message_id"}, {Name: "file_key"}},
			DoNothing: true,
		}).Create(&resource).Error; err != nil {
			return fmt.Errorf("sink resource message_id=%s key=%s: %w", message.MessageID, fileKey, err)
		}
	}
	return nil
}

func flattenMessages(messages []CLIMessage) []CLIMessage {
	flat := make([]CLIMessage, 0, len(messages))
	var appendMessage func(CLIMessage, string, string)
	appendMessage = func(message CLIMessage, rootID, parentID string) {
		replies := message.ThreadReplies
		message.ThreadReplies = nil
		if message.RootID == "" {
			message.RootID = rootID
		}
		if message.ParentID == "" {
			message.ParentID = parentID
		}
		flat = append(flat, message)
		childRoot := rootID
		if childRoot == "" {
			childRoot = message.MessageID
		}
		for _, reply := range replies {
			if reply.ThreadID == "" {
				reply.ThreadID = message.ThreadID
			}
			appendMessage(reply, childRoot, message.MessageID)
		}
	}
	for _, message := range messages {
		appendMessage(message, "", "")
	}
	return flat
}

func parseCLITime(value string, location *time.Location) (int64, error) {
	parsed, err := time.ParseInLocation(cliTimeLayout, value, location)
	if err != nil {
		return 0, err
	}
	return parsed.UnixMilli(), nil
}

func knownMessageType(messageType string) bool {
	switch messageType {
	case "text", "post", "image", "file", "audio", "media", "video", "interactive", "merge_forward", "sticker", "system":
		return true
	default:
		return false
	}
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (s *Service) beginScan(scanType string, groupID *uint64, chatID *string, windowStart *int64) (*domain.ScanRecord, error) {
	highWater := windowStart
	record := &domain.ScanRecord{
		ScanType:        scanType,
		GroupID:         groupID,
		ChatID:          chatID,
		WindowStart:     windowStart,
		Status:          "partial",
		HighWaterBefore: highWater,
		StartedAt:       s.now(),
	}
	if err := s.db.Create(record).Error; err != nil {
		return nil, fmt.Errorf("begin scan type=%s: %w", scanType, err)
	}
	return record, nil
}

func (s *Service) finishScanOK(record *domain.ScanRecord, highWaterAfter *int64) error {
	now := s.now()
	duration := int32(now.Sub(record.StartedAt).Milliseconds())
	updates := map[string]any{
		"fetched_count":    record.FetchedCount,
		"inserted_count":   record.InsertedCount,
		"page_count":       record.PageCount,
		"status":           "ok",
		"window_end":       highWaterAfter,
		"high_water_after": highWaterAfter,
		"finished_at":      now,
		"duration_ms":      duration,
	}
	if err := s.db.Model(record).Updates(updates).Error; err != nil {
		return fmt.Errorf("finish scan id=%d: %w", record.ID, err)
	}
	return nil
}

func (s *Service) finishScanError(record *domain.ScanRecord, scanErr error) error {
	now := s.now()
	duration := int32(now.Sub(record.StartedAt).Milliseconds())
	errorType := classifyError(scanErr)
	updates := map[string]any{
		"fetched_count":  record.FetchedCount,
		"inserted_count": record.InsertedCount,
		"page_count":     record.PageCount,
		"status":         "error",
		"error_type":     errorType,
		"error_message":  scanErr.Error(),
		"finished_at":    now,
		"duration_ms":    duration,
	}
	if err := s.db.Model(record).Updates(updates).Error; err != nil {
		return fmt.Errorf("finish failed scan id=%d: %w", record.ID, err)
	}
	return nil
}

func (s *Service) finishChatOK(record *domain.ScanRecord, checkpoint *domain.Checkpoint, highWater int64, lastMessageID *string) error {
	now := s.now()
	if err := s.db.Model(checkpoint).Updates(map[string]any{
		"high_water_create_time": highWater,
		"last_message_id":        lastMessageID,
		"last_scan_at":           now,
		"last_scan_status":       "ok",
		"last_error":             nil,
	}).Error; err != nil {
		return fmt.Errorf("finish checkpoint chat_id=%s: %w", checkpoint.ChatID, err)
	}
	return s.finishScanOK(record, &highWater)
}

func (s *Service) finishChatError(record *domain.ScanRecord, checkpoint *domain.Checkpoint, scanErr error) error {
	now := s.now()
	var current domain.Checkpoint
	if err := s.db.First(&current, "chat_id = ?", checkpoint.ChatID).Error; err != nil {
		return fmt.Errorf("reload failed checkpoint chat_id=%s: %w", checkpoint.ChatID, err)
	}
	if err := s.db.Model(&current).Updates(map[string]any{
		"last_scan_at":     now,
		"last_scan_status": "error",
		"last_error":       scanErr.Error(),
	}).Error; err != nil {
		return fmt.Errorf("mark checkpoint failed chat_id=%s: %w", checkpoint.ChatID, err)
	}
	if err := s.finishScanError(record, scanErr); err != nil {
		return err
	}
	if err := s.db.Model(record).Updates(map[string]any{
		"window_end":       current.HighWaterCreateTime,
		"high_water_after": current.HighWaterCreateTime,
	}).Error; err != nil {
		return fmt.Errorf("record failed scan high water id=%d: %w", record.ID, err)
	}
	return nil
}

func classifyError(err error) string {
	var apiErr *larkcli.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Subtype != "" {
			return "lark_api_" + apiErr.Subtype
		}
		return "lark_api"
	}
	var commandErr *larkcli.CommandError
	if errors.As(err, &commandErr) {
		return "lark_cli_process"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	typeName := fmt.Sprintf("%T", err)
	return strings.TrimPrefix(typeName, "*")
}
