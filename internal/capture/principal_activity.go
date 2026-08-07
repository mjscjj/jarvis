package capture

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

const principalActivityScanType = "principal_activity"

// principalGroupActivity is the earliest principal message observed in one
// group during the current search window.
type principalGroupActivity struct {
	ChatID          string
	EarliestMessage int64
}

// SyncPrincipalActivityGroups finds group/topic chats where the principal has
// spoken and permanently opts them into ordinary M2 polling. Search uses the
// user identity, so it also covers groups where Jarvis Bot is absent or was not
// mentioned. The durable cursor overlaps every run to tolerate Feishu search
// indexing delay.
func (s *Service) SyncPrincipalActivityGroups(ctx context.Context) (err error) {
	now := s.now()
	endMS := now.UnixMilli()
	checkpoint, found, err := s.loadPrincipalActivityCheckpoint()
	if err != nil {
		return err
	}
	startMS := endMS - s.opts.SearchOverlap.Milliseconds()
	if found {
		startMS = checkpoint.LastSearchAt - s.opts.SearchOverlap.Milliseconds()
	}
	if startMS < 0 {
		startMS = 0
	}
	record, err := s.beginScan(principalActivityScanType, nil, nil, &startMS, &startMS)
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

	var response MessageSearchResponse
	if err = s.lark.Run(
		ctx,
		&response,
		"im", "+messages-search",
		"--query", "",
		"--sender", s.opts.PrincipalOpenID,
		"--chat-type", "group",
		"--start", time.UnixMilli(startMS).In(s.opts.Location).Format(time.RFC3339),
		"--end", now.In(s.opts.Location).Format(time.RFC3339),
		"--page-size", strconv.Itoa(s.opts.PageSize),
		"--page-all",
		"--no-reactions",
		"--as", "user",
	); err != nil {
		return fmt.Errorf("search principal group activity: %w", err)
	}
	record.PageCount = 1
	record.FetchedCount = int32(len(response.Data.Messages))
	if response.Data.HasMore {
		return fmt.Errorf(
			"search principal group activity remained paginated after --page-all: page_token=%q",
			response.Data.PageToken,
		)
	}
	activities, err := s.principalGroupActivities(response.Data.Messages)
	if err != nil {
		return err
	}
	if err = s.ensureActivityGroupsDiscovered(ctx, activities); err != nil {
		return err
	}
	opened, err := s.openPrincipalActivityGroups(activities)
	if err != nil {
		return err
	}
	record.InsertedCount = int32(opened)
	if err = s.advancePrincipalActivityCheckpoint(checkpoint, found, endMS); err != nil {
		return err
	}
	return s.finishScanOK(record, &endMS)
}

// ScanPrincipalActivityAndRelated keeps ordinary capture healthy even when
// principal-activity discovery fails. Both errors are returned, but an auth or
// search failure must not stop chats that are already related from being
// scanned.
func (s *Service) ScanPrincipalActivityAndRelated(ctx context.Context) error {
	activityErr := s.SyncPrincipalActivityGroups(ctx)
	scanErr := s.ScanRelated(ctx)
	return errors.Join(activityErr, scanErr)
}

func (s *Service) loadPrincipalActivityCheckpoint() (*domain.PrincipalActivityCheckpoint, bool, error) {
	var checkpoint domain.PrincipalActivityCheckpoint
	result := s.db.Where("principal_open_id = ?", s.opts.PrincipalOpenID).First(&checkpoint)
	if result.Error == nil {
		return &checkpoint, true, nil
	}
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return &domain.PrincipalActivityCheckpoint{PrincipalOpenID: s.opts.PrincipalOpenID}, false, nil
	}
	return nil, false, fmt.Errorf("load principal activity checkpoint: %w", result.Error)
}

func (s *Service) principalGroupActivities(messages []SearchedMessage) ([]principalGroupActivity, error) {
	earliest := make(map[string]int64)
	for _, message := range messages {
		chatID := strings.TrimSpace(message.ChatID)
		if chatID == "" {
			return nil, fmt.Errorf("principal activity message %q has empty chat_id", message.MessageID)
		}
		// Feishu search only accepts --chat-type group|p2p, but reports topic
		// groups back as chat_type=topic. Both are group chats we want to open.
		if message.ChatType != "group" && message.ChatType != "topic" {
			return nil, fmt.Errorf(
				"principal activity message %q chat_id=%s has unexpected chat_type=%q",
				message.MessageID,
				chatID,
				message.ChatType,
			)
		}
		if message.Sender.ID != s.opts.PrincipalOpenID {
			return nil, fmt.Errorf(
				"principal activity message %q sender=%q does not match principal=%q",
				message.MessageID,
				message.Sender.ID,
				s.opts.PrincipalOpenID,
			)
		}
		createTime, err := parseCLITime(message.CreateTime, s.opts.Location)
		if err != nil {
			return nil, fmt.Errorf(
				"parse principal activity message %q create_time=%q: %w",
				message.MessageID,
				message.CreateTime,
				err,
			)
		}
		if previous, ok := earliest[chatID]; !ok || createTime < previous {
			earliest[chatID] = createTime
		}
	}
	activities := make([]principalGroupActivity, 0, len(earliest))
	for chatID, createTime := range earliest {
		activities = append(activities, principalGroupActivity{
			ChatID:          chatID,
			EarliestMessage: createTime,
		})
	}
	sort.Slice(activities, func(i, j int) bool {
		return activities[i].ChatID < activities[j].ChatID
	})
	return activities, nil
}

func (s *Service) ensureActivityGroupsDiscovered(ctx context.Context, activities []principalGroupActivity) error {
	if len(activities) == 0 {
		return nil
	}
	chatIDs := make([]string, 0, len(activities))
	for _, activity := range activities {
		chatIDs = append(chatIDs, activity.ChatID)
	}
	var count int64
	if err := s.db.Model(&domain.Group{}).Where("chat_id IN ?", chatIDs).Count(&count).Error; err != nil {
		return fmt.Errorf("count discovered principal activity groups: %w", err)
	}
	if count == int64(len(chatIDs)) {
		return nil
	}
	if err := s.DiscoverChats(ctx); err != nil {
		return fmt.Errorf("discover missing principal activity groups: %w", err)
	}
	count = 0
	if err := s.db.Model(&domain.Group{}).Where("chat_id IN ?", chatIDs).Count(&count).Error; err != nil {
		return fmt.Errorf("recount discovered principal activity groups: %w", err)
	}
	if count != int64(len(chatIDs)) {
		return fmt.Errorf(
			"principal activity groups discovered=%d, want=%d: %s",
			count,
			len(chatIDs),
			strings.Join(chatIDs, ","),
		)
	}
	return nil
}

func (s *Service) openPrincipalActivityGroups(activities []principalGroupActivity) (int, error) {
	opened := 0
	for _, activity := range activities {
		var group domain.Group
		if err := s.db.Select("id", "chat_id", "chat_mode", "related_group").
			Where("chat_id = ?", activity.ChatID).
			First(&group).Error; err != nil {
			return opened, fmt.Errorf("load principal activity group chat_id=%s: %w", activity.ChatID, err)
		}
		if group.ChatMode != "group" && group.ChatMode != "topic" {
			return opened, fmt.Errorf(
				"principal activity chat_id=%s has unsupported chat_mode=%q",
				group.ChatID,
				group.ChatMode,
			)
		}
		if group.RelatedGroup {
			continue
		}
		windowStart := activity.EarliestMessage - s.opts.ActivationContext.Milliseconds()
		if windowStart < 0 {
			windowStart = 0
		}
		checkpointUpdate := s.db.Model(&domain.Checkpoint{}).
			Where("chat_id = ?", group.ChatID).
			Updates(map[string]any{
				"high_water_create_time": windowStart,
				"last_message_id":        nil,
				"backfill_done":          true,
				"backfill_since":         windowStart,
				"last_scan_at":           nil,
				"last_scan_status":       nil,
				"last_error":             nil,
			})
		if checkpointUpdate.Error != nil {
			return opened, fmt.Errorf(
				"reset principal activity scan window chat_id=%s: %w",
				group.ChatID,
				checkpointUpdate.Error,
			)
		}
		if checkpointUpdate.RowsAffected != 1 {
			return opened, fmt.Errorf(
				"reset principal activity scan window chat_id=%s affected=%d, want=1",
				group.ChatID,
				checkpointUpdate.RowsAffected,
			)
		}
		groupUpdate := s.db.Model(&domain.Group{}).
			Where("id = ? AND related_group = ?", group.ID, false).
			Update("related_group", true)
		if groupUpdate.Error != nil {
			return opened, fmt.Errorf("open principal activity group chat_id=%s: %w", group.ChatID, groupUpdate.Error)
		}
		if groupUpdate.RowsAffected != 1 {
			return opened, fmt.Errorf(
				"open principal activity group chat_id=%s affected=%d, want=1",
				group.ChatID,
				groupUpdate.RowsAffected,
			)
		}
		opened++
	}
	return opened, nil
}

func (s *Service) advancePrincipalActivityCheckpoint(
	checkpoint *domain.PrincipalActivityCheckpoint,
	found bool,
	endMS int64,
) error {
	if !found {
		checkpoint.LastSearchAt = endMS
		if err := s.db.Create(checkpoint).Error; err != nil {
			return fmt.Errorf("create principal activity checkpoint: %w", err)
		}
		return nil
	}
	result := s.db.Model(&domain.PrincipalActivityCheckpoint{}).
		Where(
			"principal_open_id = ? AND last_search_at = ?",
			checkpoint.PrincipalOpenID,
			checkpoint.LastSearchAt,
		).
		Update("last_search_at", endMS)
	if result.Error != nil {
		return fmt.Errorf("advance principal activity checkpoint: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf(
			"advance principal activity checkpoint principal=%s affected=%d, want=1",
			checkpoint.PrincipalOpenID,
			result.RowsAffected,
		)
	}
	return nil
}
