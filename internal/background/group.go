package background

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/gorm"
)

// backgroundColumns is the exhaustive set of Group columns this package may
// write. Everything else (chat_id, name, description, tier, last_active_at, ...)
// is owned by capture (M2) and must never be touched here.
var backgroundColumns = []string{
	"background_note", "project_id", "related_group", "pinned", "include_in_memory", "is_key_group",
}

// RelatedScanTrigger lets a newly related group be scanned immediately instead
// of waiting for the next scan cron cycle. It is implemented by the capture
// service; the interface keeps background decoupled from capture at compile
// time. A nil trigger means "no immediate scan" (the cron cycle still covers it).
type RelatedScanTrigger interface {
	ScanChatNow(ctx context.Context, chatID string) error
}

// GroupList is the paginated response for groups. Broadened is set when a
// keyword search forced the query out of the related-only view (so the frontend
// can tell the user "we searched all chats, not just monitored ones").
type GroupList struct {
	Items     []GroupView `json:"items"`
	Total     int64       `json:"total"`
	Page      int         `json:"page"`
	PageSize  int         `json:"page_size"`
	Broadened bool        `json:"broadened"`
}

// GroupFilter narrows the group list. RelatedOnly is the default view (only
// chats actually being scanned); Keyword/ChatMode/Tier let the user find the
// rest without loading all thousands of discovered chats at once.
type GroupFilter struct {
	ListFilter
	RelatedOnly bool
	KeyOnly     bool
	Keyword     string
	ChatMode    string
	Tier        string
}

// GroupBackgroundService patches the human-curated subset of the feishu_group
// table. It never creates or deletes a group (capture discovery owns lifecycle).
type GroupBackgroundService struct {
	db      *gorm.DB
	trigger RelatedScanTrigger
}

// NewGroupBackgroundService wires the CRUD-only service. trigger may be nil in
// tests or CLI paths; when set, marking a group related fires an immediate scan.
func NewGroupBackgroundService(db *gorm.DB, trigger RelatedScanTrigger) (*GroupBackgroundService, error) {
	if db == nil {
		return nil, fmt.Errorf("group background service db is nil")
	}
	return &GroupBackgroundService{db: db, trigger: trigger}, nil
}

// validGroupTiers mirrors the display-only tier labels written by capture.
var validGroupTiers = map[string]struct{}{"hot": {}, "warm": {}, "cold": {}}

// validChatModes mirrors the chat_mode values capture persists.
var validChatModes = map[string]struct{}{"group": {}, "p2p": {}, "topic": {}}

func (f GroupFilter) validate() error {
	if err := f.ListFilter.validate(); err != nil {
		return err
	}
	if f.Tier != "" {
		if _, ok := validGroupTiers[f.Tier]; !ok {
			return fmt.Errorf("group tier %q is invalid", f.Tier)
		}
	}
	if f.ChatMode != "" {
		if _, ok := validChatModes[f.ChatMode]; !ok {
			return fmt.Errorf("group chat_mode %q is invalid", f.ChatMode)
		}
	}
	return nil
}

// broadened reports whether a keyword search should override the related-only
// view. With ~1500 discovered chats, a user who types a keyword almost always
// wants to find a not-yet-monitored chat, so a keyword implicitly widens the
// scope to all chats; without a keyword the related-only default is respected.
func (f GroupFilter) broadened() bool {
	return f.RelatedOnly && f.Keyword != ""
}

// applyFilters builds the shared WHERE clauses for both count and list queries.
// Keyword search spans the group's own columns (name/chat_id/description/
// owner_open_id) plus the joined project name and group-owner person name, so a
// user can find a chat by "who owns it" or "which project it belongs to" — not
// just by a name that is frequently NULL for p2p/topic chats.
func (f GroupFilter) applyFilters(query *gorm.DB) *gorm.DB {
	if f.RelatedOnly && !f.broadened() {
		query = query.Where("feishu_group.related_group = ?", true)
	}
	if f.KeyOnly {
		query = query.Where("feishu_group.is_key_group = ?", true)
	}
	if f.ChatMode != "" {
		query = query.Where("feishu_group.chat_mode = ?", f.ChatMode)
	}
	if f.Tier != "" {
		query = query.Where("feishu_group.tier = ?", f.Tier)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		query = query.
			Joins("LEFT JOIN project ON project.id = feishu_group.project_id").
			Joins("LEFT JOIN person ON person.open_id = feishu_group.owner_open_id").
			Where(
				"feishu_group.name LIKE ? OR feishu_group.chat_id LIKE ? OR feishu_group.description LIKE ? OR feishu_group.owner_open_id LIKE ? OR project.name LIKE ? OR person.name LIKE ?",
				like, like, like, like, like, like,
			)
	}
	return query
}

func (s *GroupBackgroundService) List(ctx context.Context, filter GroupFilter) (*GroupList, error) {
	if err := filter.validate(); err != nil {
		return nil, invalid(err)
	}
	// owner_open_id is a unique index on person, so the LEFT JOINs cannot fan a
	// group into multiple rows; COUNT over the group primary key stays exact.
	var total int64
	if err := filter.applyFilters(s.db.WithContext(ctx).Model(&domain.Group{})).
		Distinct("feishu_group.id").Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count groups: %w", err)
	}
	items := make([]domain.Group, 0, filter.PageSize)
	if total > 0 {
		if err := filter.applyFilters(s.db.WithContext(ctx).Preload("Project")).
			Select("feishu_group.*").
			Order("feishu_group.related_group DESC, feishu_group.is_key_group DESC, feishu_group.pinned DESC, feishu_group.last_active_at DESC").
			Limit(filter.PageSize).
			Offset(filter.offset()).
			Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list groups: %w", err)
		}
	}
	views, err := s.enrichGroupViews(ctx, items)
	if err != nil {
		return nil, err
	}
	return &GroupList{
		Items: views, Total: total, Page: filter.Page,
		PageSize: filter.PageSize, Broadened: filter.broadened(),
	}, nil
}

// enrichGroupViews attaches per-chat scan state (last_scan_at/last_scan_status
// from chat_checkpoint) and captured message counts. These are read-only
// observability fields, fetched in two batch queries to avoid N+1.
func (s *GroupBackgroundService) enrichGroupViews(ctx context.Context, groups []domain.Group) ([]GroupView, error) {
	views := toGroupViews(groups)
	if len(groups) == 0 {
		return views, nil
	}
	chatIDs := make([]string, len(groups))
	for i := range groups {
		chatIDs[i] = groups[i].ChatID
	}

	var checkpoints []domain.Checkpoint
	if err := s.db.WithContext(ctx).
		Select("chat_id", "last_scan_at", "last_scan_status").
		Where("chat_id IN ?", chatIDs).
		Find(&checkpoints).Error; err != nil {
		return nil, fmt.Errorf("load group scan state: %w", err)
	}
	scanByChat := make(map[string]domain.Checkpoint, len(checkpoints))
	for _, cp := range checkpoints {
		scanByChat[cp.ChatID] = cp
	}

	type countRow struct {
		ChatID string
		Count  int64
	}
	var counts []countRow
	if err := s.db.WithContext(ctx).
		Model(&domain.Message{}).
		Select("chat_id, COUNT(*) AS count").
		Where("chat_id IN ?", chatIDs).
		Group("chat_id").
		Scan(&counts).Error; err != nil {
		return nil, fmt.Errorf("count group messages: %w", err)
	}
	countByChat := make(map[string]int64, len(counts))
	for _, row := range counts {
		countByChat[row.ChatID] = row.Count
	}

	for i := range views {
		if cp, ok := scanByChat[views[i].ChatID]; ok {
			views[i].LastScanAt = cp.LastScanAt
			views[i].LastScanStatus = cp.LastScanStatus
		}
		views[i].MessageCount = countByChat[views[i].ChatID]
	}
	return views, nil
}

// UpdateBackground patches only the curated columns of one existing group.
// When it flips related_group from false to true, it fires an immediate scan so
// the newly monitored chat starts capturing without waiting for the scan cron.
func (s *GroupBackgroundService) UpdateBackground(ctx context.Context, id uint64, in GroupBackgroundInput) (*GroupView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("group id must be positive"))
	}
	if in.ProjectID != nil {
		if *in.ProjectID == 0 {
			return nil, invalid(fmt.Errorf("group project_id must be positive when provided"))
		}
		var count int64
		if err := s.db.WithContext(ctx).Model(&domain.Project{}).Where("id = ?", *in.ProjectID).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("verify project id=%d: %w", *in.ProjectID, err)
		}
		if count == 0 {
			return nil, invalid(fmt.Errorf("group project_id=%d does not exist", *in.ProjectID))
		}
	}

	var previous domain.Group
	err := s.db.WithContext(ctx).Select("id", "chat_id", "related_group").Where("id = ?", id).Take(&previous).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load group id=%d: %w", id, err)
	}

	updates := map[string]any{
		"background_note":   in.BackgroundNote,
		"project_id":        in.ProjectID,
		"related_group":     in.RelatedGroup,
		"pinned":            in.Pinned,
		"include_in_memory": in.IncludeInMemory,
		"is_key_group":      in.IsKeyGroup,
	}
	result := s.db.WithContext(ctx).
		Model(&domain.Group{}).
		Where("id = ?", id).
		Select(backgroundColumns).
		Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update group background id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}

	if s.trigger != nil && in.RelatedGroup && !previous.RelatedGroup {
		s.triggerScan(ctx, previous.ChatID)
	}
	return s.get(ctx, id)
}

// triggerScan fires a best-effort immediate scan for a freshly related chat on
// its own goroutine and context, so a slow lark-cli call never blocks the HTTP
// response. Failures are logged only; the scan cron cycle is the safety net.
func (s *GroupBackgroundService) triggerScan(parent context.Context, chatID string) {
	detached := observability.Detached(parent)
	go func() {
		ctx, cancel := context.WithTimeout(detached, 2*time.Minute)
		defer cancel()
		if err := s.trigger.ScanChatNow(ctx, chatID); err != nil {
			hlog.CtxErrorf(ctx, "background immediate scan chat_id=%s status=error error=%+v", chatID, err)
			return
		}
		hlog.CtxInfof(ctx, "background immediate scan chat_id=%s status=ok", chatID)
	}()
}

func (s *GroupBackgroundService) get(ctx context.Context, id uint64) (*GroupView, error) {
	var group domain.Group
	err := s.db.WithContext(ctx).Preload("Project").Where("id = ?", id).Take(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get group id=%d: %w", id, err)
	}
	view := toGroupView(&group)
	return &view, nil
}

// GetByChatID looks a group up by its Feishu chat_id (the business key),
// preloading the bound project. It is used by the jarvis-tools CLI so codex can
// read the group announcement (description) and bound project during extraction.
func (s *GroupBackgroundService) GetByChatID(ctx context.Context, chatID string) (*GroupView, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, invalid(fmt.Errorf("group chat_id must not be empty"))
	}
	var group domain.Group
	err := s.db.WithContext(ctx).Preload("Project").Where("chat_id = ?", chatID).Take(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get group chat_id=%s: %w", chatID, err)
	}
	view := toGroupView(&group)
	return &view, nil
}
