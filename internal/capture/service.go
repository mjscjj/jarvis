package capture

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/larkcli"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	cliTimeLayout   = "2006-01-02 15:04"
	inactiveChatAge = 5 * 24 * time.Hour
)

type runner interface {
	Run(ctx context.Context, out any, args ...string) error
}

// ChatScanResult is emitted only after one chat's messages and checkpoint have
// committed successfully. The observer uses it as an acceleration signal; the
// database remains the recovery source if delivery fails or the process exits.
type ChatScanResult struct {
	ChatID        string
	InsertedCount int32
	MessageIDs    []string
	HighWater     int64
	LastMessageID *string
}

// ScanObserver receives successful scans that inserted at least one new message.
// It intentionally lives in capture so M2 does not import the downstream pipeline.
type ScanObserver interface {
	ChatScanned(context.Context, ChatScanResult) error
}

// Options contains capture policy already decided by the technical design.
type Options struct {
	PageSize          int
	ScanWorkers       int
	HotAge            time.Duration
	WarmAge           time.Duration
	Location          *time.Location
	PrincipalOpenID   string
	SearchOverlap     time.Duration
	ActivationContext time.Duration
	// AutoRelatedP2PTopN 是 discover 自动纳入监听的内部真人私聊上限（按 active_time
	// 取最活跃的前 N 个）。0 表示不自动开任何私聊（全靠手动名单）。
	AutoRelatedP2PTopN int
}

// Service owns conversation discovery and polling state transitions.
type Service struct {
	db       *gorm.DB
	lark     runner
	opts     Options
	now      func() time.Time
	observer ScanObserver
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
	if strings.TrimSpace(opts.PrincipalOpenID) == "" {
		return nil, fmt.Errorf("capture principal open_id is empty")
	}
	if opts.SearchOverlap <= 0 {
		return nil, fmt.Errorf("capture search overlap must be positive")
	}
	if opts.ActivationContext <= 0 {
		return nil, fmt.Errorf("capture activation context must be positive")
	}
	if opts.AutoRelatedP2PTopN < 0 {
		return nil, fmt.Errorf("capture auto-related p2p top-n must be non-negative")
	}
	return &Service{db: db, lark: lark, opts: opts, now: time.Now}, nil
}

// SetScanObserver wires the process-level pipeline before schedulers and HTTP
// handlers start. Replacing an active observer is rejected so runtime behavior
// cannot change underneath an in-flight scan.
func (s *Service) SetScanObserver(observer ScanObserver) error {
	if observer == nil {
		return fmt.Errorf("capture scan observer is nil")
	}
	if s.observer != nil {
		return fmt.Errorf("capture scan observer is already set")
	}
	s.observer = observer
	return nil
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
//
// 存量 p2p 的 checkpoint 水位停在"当年被发现那一刻"，若不处理，下一轮
// scan_related 会从那个久远时间点开始 asc 回捞历史。这里以"纳入监听那一刻"
// 为起始水位：先把这批 p2p 的 high_water_create_time 抬到 now（仅抬落后于
// now 的），再置 related_group=1。顺序如此是为了规避半成功——即使抬水位后
// 置位失败，这批仍未 related，重跑时能再次命中并抬水位，不会漏抬。
func (s *Service) OpenInternalP2P() (int64, error) {
	nowMS := s.now().UnixMilli()

	var chatIDs []string
	if err := s.db.Model(&domain.Group{}).
		Where("chat_mode = ? AND external = ? AND related_group = ? AND p2p_target_type = ?", "p2p", false, false, "user").
		Pluck("chat_id", &chatIDs).Error; err != nil {
		return 0, fmt.Errorf("list internal p2p chats to open: %w", err)
	}
	if len(chatIDs) == 0 {
		return 0, nil
	}

	if err := s.db.Model(&domain.Checkpoint{}).
		Where("chat_id IN ? AND high_water_create_time < ?", chatIDs, nowMS).
		Update("high_water_create_time", nowMS).Error; err != nil {
		return 0, fmt.Errorf("advance internal p2p scan window: %w", err)
	}

	result := s.db.Model(&domain.Group{}).
		Where("chat_id IN ?", chatIDs).
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

// DiscoverChats enumerates every user-visible chat. Automatic p2p monitoring is
// reconciled only after the complete active_time-sorted list has been fetched
// successfully, so a failed partial listing can never evict a currently
// monitored conversation. A newly activated p2p receives only the bounded
// ActivationContext window needed to capture the message that made it active.
func (s *Service) DiscoverChats(ctx context.Context) (err error) {
	record, err := s.beginScan("discover", nil, nil, nil, nil)
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

	rankedP2P := make([]string, 0, s.opts.AutoRelatedP2PTopN)
	seenP2P := make(map[string]struct{})
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
		for _, chat := range response.Data.Chats {
			if !isAutoRelatedP2P(chat) {
				continue
			}
			if _, duplicated := seenP2P[chat.ChatID]; duplicated {
				return fmt.Errorf("chat list returned duplicated internal p2p chat_id=%s", chat.ChatID)
			}
			seenP2P[chat.ChatID] = struct{}{}
			rankedP2P = append(rankedP2P, chat.ChatID)
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
	if err = s.reconcileAutoRelatedP2P(rankedP2P); err != nil {
		return err
	}
	if err = s.recomputeTiers(); err != nil {
		return err
	}
	return s.finishScanOK(record, nil)
}

// isAutoRelatedP2P 判定一条私聊是否是"自动纳入监听"的候选：内部(external=false)
// 真人(p2p_target_type=user)私聊。服务号/机器人私聊(target_type=bot)即便 external
// 为 false 也排除——跟它们聊不出 todo，只会白占 TopN 名额。
func isAutoRelatedP2P(chat CLIChat) bool {
	return chat.ChatMode == "p2p" && !chat.External && chat.P2PTargetType == "user"
}

// persistDiscoveredChats upserts one page of chat metadata without changing the
// monitoring set. DiscoverChats owns that transition after every page succeeds.
// Fail-fast: unknown chat_mode aborts the page.
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
			group := domain.Group{
				ChatID:        chat.ChatID,
				ChatMode:      chat.ChatMode,
				Name:          nullableString(chat.Name),
				Description:   nullableString(chat.Description),
				OwnerOpenID:   nullableString(chat.OwnerID),
				External:      chat.External,
				TenantKey:     nullableString(chat.TenantKey),
				P2PTargetType: nullableString(chat.P2PTargetType),
				RelatedGroup:  false,
				Tier:          "cold",
			}
			// 元数据 upsert 不含 related_group：分页期间不触碰监听集合。
			updateColumns := []string{
				"chat_mode", "name", "description", "owner_open_id", "external", "tenant_key", "p2p_target_type", "updated_at",
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

// reconcileAutoRelatedP2P makes the automatic set equal to the current
// active_time Top-N. Pinned p2p chats are human-curated fixed monitoring entries:
// they are preserved and do not consume the automatic budget. Unpinned internal
// human p2p chats are the rotating set.
func (s *Service) reconcileAutoRelatedP2P(rankedChatIDs []string) error {
	var groups []domain.Group
	if err := s.db.Select("chat_id", "related_group", "pinned").
		Where("chat_mode = ? AND external = ? AND p2p_target_type = ?", "p2p", false, "user").
		Find(&groups).Error; err != nil {
		return fmt.Errorf("list internal p2p chats for monitoring reconciliation: %w", err)
	}
	byChatID := make(map[string]domain.Group, len(groups))
	for _, group := range groups {
		byChatID[group.ChatID] = group
	}

	desired := make([]string, 0, s.opts.AutoRelatedP2PTopN)
	for _, chatID := range rankedChatIDs {
		group, ok := byChatID[chatID]
		if !ok {
			return fmt.Errorf("ranked internal p2p chat_id=%s was not persisted", chatID)
		}
		if group.Pinned {
			continue
		}
		if len(desired) == s.opts.AutoRelatedP2PTopN {
			break
		}
		desired = append(desired, chatID)
	}

	if len(desired) > 0 {
		activationStart := s.now().Add(-s.opts.ActivationContext).UnixMilli()
		if err := s.db.Model(&domain.Checkpoint{}).
			Where("chat_id IN ? AND last_scan_at IS NULL", desired).
			Update("high_water_create_time", activationStart).Error; err != nil {
			return fmt.Errorf("set newly monitored p2p activation windows: %w", err)
		}
	}

	query := s.db.Model(&domain.Group{}).
		Where("chat_mode = ? AND external = ? AND p2p_target_type = ? AND pinned = ?", "p2p", false, "user", false)
	var result *gorm.DB
	if len(desired) == 0 {
		result = query.Update("related_group", false)
	} else {
		result = query.Update("related_group", gorm.Expr("CASE WHEN chat_id IN ? THEN ? ELSE ? END", desired, true, false))
	}
	if result.Error != nil {
		return fmt.Errorf("reconcile automatic p2p monitoring set: %w", result.Error)
	}
	return nil
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

// ensureScanWindow bounds the first scan of a manually related chat. Groups
// start at now; p2p chats receive the short ActivationContext window so the
// message that prompted manual monitoring is not skipped.
func (s *Service) ensureScanWindow(chatID string) error {
	var group domain.Group
	if err := s.db.Select("id", "chat_mode", "last_active_at").Where("chat_id = ?", chatID).First(&group).Error; err != nil {
		return fmt.Errorf("load group chat_id=%s: %w", chatID, err)
	}
	if group.LastActiveAt != nil {
		return nil
	}
	windowStart := s.now().UnixMilli()
	if group.ChatMode == "p2p" {
		windowStart = s.now().Add(-s.opts.ActivationContext).UnixMilli()
	}
	if err := s.db.Model(&domain.Checkpoint{}).
		Where("chat_id = ?", chatID).
		Update("high_water_create_time", windowStart).Error; err != nil {
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
	searchWindowStart := windowStart
	if isGroupConversation(group.ChatMode) {
		searchWindowStart -= s.opts.SearchOverlap.Milliseconds()
		if searchWindowStart < 0 {
			searchWindowStart = 0
		}
	}
	record, err := s.beginScan(scanType, &group.ID, &chatID, &searchWindowStart, &windowStart)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if err != nil && !committed {
			if finishErr := s.finishChatError(record, &checkpoint, err); finishErr != nil {
				err = errors.Join(err, finishErr)
			}
		}
	}()

	pageToken := ""
	currentHW := windowStart
	lastMessageID := checkpoint.LastMessageID
	insertedMessageIDs := make([]string, 0)
	if isGroupConversation(group.ChatMode) {
		var messages []CLIMessage
		messages, record.PageCount, err = s.searchGroupMessages(ctx, chatID, searchWindowStart, s.now())
		record.FetchedCount = int32(len(messages))
		if err != nil {
			return err
		}
		var inserted int32
		inserted, insertedMessageIDs, currentHW, lastMessageID, err = s.persistMessagePage(
			&group,
			messages,
			currentHW,
			lastMessageID,
		)
		if err != nil {
			return fmt.Errorf("persist group messages chat_id=%s: %w", chatID, err)
		}
		record.InsertedCount = inserted
	} else {
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
			var (
				inserted    int32
				insertedIDs []string
			)
			inserted, insertedIDs, currentHW, lastMessageID, err = s.persistMessagePage(&group, messages, currentHW, lastMessageID)
			if err != nil {
				return fmt.Errorf("persist messages chat_id=%s page=%d: %w", chatID, record.PageCount+1, err)
			}
			record.FetchedCount += int32(len(messages))
			record.InsertedCount += inserted
			insertedMessageIDs = append(insertedMessageIDs, insertedIDs...)
			record.PageCount++
			if !response.Data.HasMore {
				break
			}
			if response.Data.PageToken == "" {
				return fmt.Errorf("message list chat_id=%s page=%d has_more=true with empty page_token", chatID, record.PageCount)
			}
			pageToken = response.Data.PageToken
		}
	}

	if err = s.finishChatOK(record, &checkpoint, currentHW, lastMessageID); err != nil {
		return err
	}
	if err = s.recomputeGroupTier(group.ID); err != nil {
		return err
	}
	committed = true
	if !group.Pinned && currentHW <= s.now().Add(-inactiveChatAge).UnixMilli() {
		if err = s.db.Model(&domain.Group{}).
			Where("id = ? AND related_group = ? AND pinned = ?", group.ID, true, false).
			Update("related_group", false).Error; err != nil {
			return fmt.Errorf("remove inactive chat from monitoring chat_id=%s: %w", chatID, err)
		}
	}
	if record.InsertedCount == 0 || s.observer == nil {
		return nil
	}
	return s.observer.ChatScanned(ctx, ChatScanResult{
		ChatID: chatID, InsertedCount: record.InsertedCount,
		MessageIDs: insertedMessageIDs, HighWater: currentHW,
		LastMessageID: cloneString(lastMessageID),
	})
}

func isGroupConversation(chatMode string) bool {
	return chatMode == "group" || chatMode == "topic"
}

// searchGroupMessages reads group and topic messages by their own create time.
// Feishu's chat listing filters thread roots by root create time, including in
// regular groups, so a new reply under an old root is otherwise invisible after
// the root passes the chat checkpoint.
//
// Search results are not ordered. Fetch every page before persisting anything,
// then sort deterministically so a failed or partial search cannot advance the
// checkpoint past an unseen reply. The overlap tolerates search-index delay;
// message_id remains the idempotency key when old rows are returned again.
func (s *Service) searchGroupMessages(
	ctx context.Context,
	chatID string,
	searchStart int64,
	windowEnd time.Time,
) ([]CLIMessage, int32, error) {
	pageToken := ""
	seenPageTokens := make(map[string]struct{})
	messages := make([]CLIMessage, 0)
	var pageCount int32
	for {
		var response MessageSearchListResponse
		args := []string{
			"im", "+messages-search", "--query", "", "--chat-id", chatID,
			"--start", time.UnixMilli(searchStart).In(s.opts.Location).Format(time.RFC3339),
			"--end", windowEnd.In(s.opts.Location).Format(time.RFC3339),
			"--page-size", strconv.Itoa(s.opts.PageSize), "--no-reactions", "--as", "user",
		}
		if pageToken != "" {
			args = append(args, "--page-token", pageToken)
		}
		if err := s.lark.Run(ctx, &response, args...); err != nil {
			return messages, pageCount, fmt.Errorf(
				"search group messages chat_id=%s page=%d: %w",
				chatID,
				pageCount+1,
				err,
			)
		}
		pageCount++
		messages = append(messages, response.Data.Messages...)
		if !response.Data.HasMore {
			break
		}
		nextPageToken := response.Data.PageToken
		if nextPageToken == "" {
			return messages, pageCount, fmt.Errorf(
				"search group messages chat_id=%s page=%d has_more=true with empty page_token",
				chatID,
				pageCount,
			)
		}
		if _, exists := seenPageTokens[nextPageToken]; exists {
			return messages, pageCount, fmt.Errorf(
				"search group messages chat_id=%s page=%d repeated page_token=%q",
				chatID,
				pageCount,
				nextPageToken,
			)
		}
		seenPageTokens[nextPageToken] = struct{}{}
		pageToken = nextPageToken
	}
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].CreateTime == messages[j].CreateTime {
			return messages[i].MessageID < messages[j].MessageID
		}
		return messages[i].CreateTime < messages[j].CreateTime
	})
	return messages, pageCount, nil
}

// ScanRelated scans every related chat in one pass. Tier no longer gates
// scheduling: all related chats share the single scan cadence. Chat failures
// are collected and returned after the other chats finish. Capture failures do
// not advance the checkpoint; an observer failure is reported only after the
// scan committed and is recovered by the downstream compensation schedule.
func (s *Service) ScanRelated(ctx context.Context) error {
	var groups []domain.Group
	if err := s.db.Select("id", "chat_id").
		Where("related_group = ? AND chat_mode IN ?", true, []string{"group", "topic", "p2p"}).
		Order("id ASC").Find(&groups).Error; err != nil {
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

func (s *Service) persistMessagePage(group *domain.Group, messages []CLIMessage, currentHW int64, lastMessageID *string) (int32, []string, int64, *string, error) {
	var inserted int32
	insertedMessageIDs := make([]string, 0)
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
				insertedMessageIDs = append(insertedMessageIDs, message.MessageID)
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
	return inserted, insertedMessageIDs, currentHW, lastMessageID, err
}

// systemSenderOpenID 是飞书群系统消息（无真实发送者）的占位 sender，便于后续区分
// 与过滤（M3 抽取可按 sender_type=system 忽略）。
const systemSenderOpenID = "__system__"

// systemMessageType 是飞书群系统消息的 msg_type，如入退群/群设置变更/撤回等通知。
// 这类消息 sender 全空，用它判定而非 message_id 前缀（系统消息前缀同为 om_）。
const systemMessageType = "system"

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
	senderType := item.Sender.SenderType
	senderName := item.Sender.Name
	// 飞书群系统消息（msg_type=system，如"XX invited YY to the group"、入退群/群设置
	// 变更/撤回等自动通知）本就没有 sender，属正常现象而非 bug。用占位 sender 落库以
	// 保留历史，避免一条系统消息拖垮整批采集。注意其 message_id 前缀仍是 om_，只能靠
	// msg_type 判定，不能靠 message_id 前缀。
	if senderID == "" && item.MessageType == systemMessageType {
		senderID = systemSenderOpenID
		if senderType == "" {
			senderType = systemMessageType
		}
		if senderName == "" {
			senderName = "系统消息"
		}
	}
	if senderID == "" {
		return nil, fmt.Errorf("message %s sender id is empty", item.MessageID)
	}
	return &domain.Message{
		MessageID:    item.MessageID,
		ChatID:       group.ChatID,
		GroupID:      &group.ID,
		ChatMode:     group.ChatMode,
		SenderOpenID: senderID,
		SenderName:   senderName,
		SenderType:   senderType,
		MessageType:  item.MessageType,
		Content:      item.Content,
		ReplyTo:      nullableString(item.ParentID),
		RootID:       nullableString(item.RootID),
		ThreadID:     nullableString(item.ThreadID),
		CreateTime:   createTime,
		UpdateTime:   updateTime,
		Source:       "poll",
		RenderOK:     knownMessageType(item.MessageType),
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
		"content":        incoming.Content,
		"content_raw":    incoming.ContentRaw,
		"message_type":   incoming.MessageType,
		"sender_open_id": incoming.SenderOpenID,
		"sender_name":    incoming.SenderName,
		"sender_type":    incoming.SenderType,
		"update_time":    incoming.UpdateTime,
		"render_ok":      incoming.RenderOK,
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

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (s *Service) beginScan(
	scanType string,
	groupID *uint64,
	chatID *string,
	windowStart *int64,
	highWaterBefore *int64,
) (*domain.ScanRecord, error) {
	record := &domain.ScanRecord{
		ScanType:        scanType,
		GroupID:         groupID,
		ChatID:          chatID,
		WindowStart:     windowStart,
		Status:          "partial",
		HighWaterBefore: highWaterBefore,
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
