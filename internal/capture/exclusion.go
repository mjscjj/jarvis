package capture

import (
	"context"
	"fmt"

	"jarvis/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetCaptureExclusion applies the principal's durable privacy choice to a
// bounded, explicit set of discovered conversations. Excluding also clears
// every monitoring flag. Removing an exclusion restores explicit monitoring
// for eligible conversations in the same operation, so there is no ambiguous
// unexcluded state that an automatic policy could silently claim later.
func (s *Service) SetCaptureExclusion(ctx context.Context, groupIDs []uint64, excluded bool) (int64, error) {
	if len(groupIDs) == 0 {
		return 0, fmt.Errorf("capture exclusion group_ids is empty")
	}
	if len(groupIDs) > 100 {
		return 0, fmt.Errorf("capture exclusion accepts at most 100 groups")
	}
	seen := make(map[uint64]struct{}, len(groupIDs))
	for _, id := range groupIDs {
		if id == 0 {
			return 0, fmt.Errorf("capture exclusion group_id must be positive")
		}
		if _, exists := seen[id]; exists {
			return 0, fmt.Errorf("capture exclusion group_id is duplicated: %d", id)
		}
		seen[id] = struct{}{}
	}

	var groups []domain.Group
	if err := s.db.WithContext(ctx).Select("id", "chat_id", "chat_mode", "external", "capture_excluded").Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
		return 0, fmt.Errorf("load capture exclusion groups: %w", err)
	}
	if len(groups) != len(groupIDs) {
		return 0, fmt.Errorf("capture exclusion contains undiscovered group ids")
	}
	for _, group := range groups {
		if group.ChatMode != "group" && group.ChatMode != "topic" && group.ChatMode != "p2p" {
			return 0, fmt.Errorf("capture exclusion does not support chat_mode=%q", group.ChatMode)
		}
		if !excluded && !group.CaptureExcluded {
			return 0, fmt.Errorf("group id=%d is not excluded from background capture", group.ID)
		}
	}

	if excluded {
		result := s.db.WithContext(ctx).Model(&domain.Group{}).Where("id IN ?", groupIDs).Updates(map[string]any{
			"capture_excluded": true,
			"related_group":    false,
			"pinned":           false,
		})
		if result.Error != nil {
			return 0, fmt.Errorf("exclude groups from background capture: %w", result.Error)
		}
		return result.RowsAffected, nil
	}

	// Advance every cursor before making a conversation eligible again. This
	// ordering is deliberately fail-safe: a partial failure leaves the group
	// excluded and a retry can safely repeat the cursor update.
	nowMS := s.now().UnixMilli()
	checkpoints := make([]domain.Checkpoint, 0, len(groups))
	chatIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		chatIDs = append(chatIDs, group.ChatID)
		checkpoints = append(checkpoints, domain.Checkpoint{
			ChatID: group.ChatID, HighWaterCreateTime: nowMS, BackfillDone: true, BackfillSince: nowMS,
		})
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&checkpoints).Error; err != nil {
		return 0, fmt.Errorf("initialize restored capture checkpoints: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&domain.Checkpoint{}).Where("chat_id IN ?", chatIDs).Updates(map[string]any{
		"high_water_create_time": nowMS,
		"backfill_since":         nowMS,
		"capture_floor":          nowMS,
		"backfill_done":          true,
		"last_message_id":        nil,
		"last_scan_at":           nil,
		"last_scan_status":       nil,
		"last_error":             nil,
	}).Error; err != nil {
		return 0, fmt.Errorf("advance restored capture checkpoints: %w", err)
	}
	result := s.db.WithContext(ctx).Model(&domain.Group{}).Where("id IN ?", groupIDs).Updates(map[string]any{
		"capture_excluded": false,
		"related_group": gorm.Expr(
			"CASE WHEN chat_mode = ? AND external = ? THEN ? ELSE ? END",
			"p2p", true, false, true,
		),
		"pinned": gorm.Expr(
			"CASE WHEN chat_mode = ? AND external = ? THEN ? ELSE ? END",
			"p2p", true, false, true,
		),
	})
	if result.Error != nil {
		return 0, fmt.Errorf("restore groups to capture eligibility: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// disableAutomaticP2P keeps the explicit "automatic direct messages off"
// policy from becoming deferred backfill. Manually pinned direct messages stay
// monitored and excluded chats retain the cursor chosen when they were
// excluded.
func (s *Service) disableAutomaticP2P(ctx context.Context) error {
	nowMS := s.now().UnixMilli()
	var chatIDs []string
	if err := s.db.WithContext(ctx).Model(&domain.Group{}).
		Where("chat_mode = ? AND pinned = ? AND capture_excluded = ?", "p2p", false, false).
		Pluck("chat_id", &chatIDs).Error; err != nil {
		return fmt.Errorf("list automatic p2p chats to disable: %w", err)
	}
	if len(chatIDs) == 0 {
		return nil
	}
	checkpoints := make([]domain.Checkpoint, 0, len(chatIDs))
	for _, chatID := range chatIDs {
		checkpoints = append(checkpoints, domain.Checkpoint{
			ChatID: chatID, HighWaterCreateTime: nowMS, BackfillDone: true, BackfillSince: nowMS,
		})
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&checkpoints).Error; err != nil {
		return fmt.Errorf("initialize disabled automatic p2p checkpoints: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&domain.Checkpoint{}).Where("chat_id IN ?", chatIDs).Updates(map[string]any{
		"high_water_create_time": nowMS, "backfill_since": nowMS, "capture_floor": nowMS, "last_message_id": nil,
	}).Error; err != nil {
		return fmt.Errorf("advance disabled automatic p2p checkpoints: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&domain.Group{}).
		Where("chat_id IN ?", chatIDs).Update("related_group", false).Error; err != nil {
		return fmt.Errorf("disable automatic p2p monitoring: %w", err)
	}
	return nil
}

// AdvanceAutomaticP2PCheckpoints closes the privacy gap immediately before a
// saved policy re-enables automatic direct-message monitoring.
func (s *Service) AdvanceAutomaticP2PCheckpoints(ctx context.Context) error {
	return s.disableAutomaticP2P(ctx)
}
