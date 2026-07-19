package background

import (
	"context"
	"errors"
	"fmt"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// backgroundColumns is the exhaustive set of Group columns this package may
// write. Everything else (chat_id, name, description, tier, last_active_at, ...)
// is owned by capture (M2) and must never be touched here.
var backgroundColumns = []string{
	"project_id", "related_group", "pinned", "include_in_memory", "is_key_group",
}

// GroupList is the paginated response for groups.
type GroupList struct {
	Items    []GroupView `json:"items"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

// GroupFilter narrows the group list to the ones worth curating.
type GroupFilter struct {
	ListFilter
	RelatedOnly bool
}

// GroupBackgroundService patches the human-curated subset of the feishu_group
// table. It never creates or deletes a group (capture discovery owns lifecycle).
type GroupBackgroundService struct {
	db *gorm.DB
}

func NewGroupBackgroundService(db *gorm.DB) (*GroupBackgroundService, error) {
	if db == nil {
		return nil, fmt.Errorf("group background service db is nil")
	}
	return &GroupBackgroundService{db: db}, nil
}

func (s *GroupBackgroundService) List(ctx context.Context, filter GroupFilter) (*GroupList, error) {
	if err := filter.ListFilter.validate(); err != nil {
		return nil, invalid(err)
	}
	query := s.db.WithContext(ctx).Model(&domain.Group{})
	if filter.RelatedOnly {
		query = query.Where("related_group = ?", true)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count groups: %w", err)
	}
	items := make([]domain.Group, 0, filter.PageSize)
	if total > 0 {
		listQuery := s.db.WithContext(ctx).Preload("Project")
		if filter.RelatedOnly {
			listQuery = listQuery.Where("related_group = ?", true)
		}
		if err := listQuery.
			Order("is_key_group DESC, pinned DESC, last_active_at DESC").
			Limit(filter.PageSize).
			Offset(filter.offset()).
			Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list groups: %w", err)
		}
	}
	return &GroupList{Items: toGroupViews(items), Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

// UpdateBackground patches only the curated columns of one existing group.
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
	updates := map[string]any{
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
	return s.get(ctx, id)
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
