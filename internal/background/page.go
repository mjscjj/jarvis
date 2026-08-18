package background

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// PageView is the whole-page read model for one world entity.
type PageView struct {
	Type           string      `json:"type"`
	ID             uint64      `json:"id"`
	Name           string      `json:"name"`
	Summary        string      `json:"summary"`
	CharCount      int         `json:"char_count"`
	MaxChars       int         `json:"max_chars"`
	UpdatedAt      time.Time   `json:"updated_at"`
	LastProgressAt *time.Time  `json:"last_progress_at"`
	Outgoing       []Reference `json:"outgoing"`
	Backlinks      []Backlink  `json:"backlinks"`
	FactCount      int64       `json:"fact_count"`
}

// PageIndexItem is one row of the page index.
type PageIndexItem struct {
	Type           string     `json:"type"`
	ID             uint64     `json:"id"`
	Name           string     `json:"name"`
	IndexLine      string     `json:"index_line"`
	CharCount      int        `json:"char_count"`
	LastProgressAt *time.Time `json:"last_progress_at"`
}

// ListPagesFilter controls the page index. Default is the active working set.
type ListPagesFilter struct {
	Type      string
	All       bool
	StaleDays *int
	OverLimit bool
}

// UpdatePageInput is the only write path for an entity's long-term summary.
type UpdatePageInput struct {
	Content          string    `json:"content"`
	IfUnchangedSince time.Time `json:"if_unchanged_since"`
}

// PageConflictError is a CAS miss. The API maps it to HTTP 409 and returns Current.
type PageConflictError struct {
	Current *PageView
}

func (e *PageConflictError) Error() string {
	return "page was changed since if_unchanged_since; reload and merge"
}

func (e *PageConflictError) Unwrap() error { return ErrConflict }

// PageService reads and writes the six entity summary pages.
type PageService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewPageService(db *gorm.DB) (*PageService, error) {
	if db == nil {
		return nil, fmt.Errorf("page service db is nil")
	}
	return &PageService{db: db, now: time.Now}, nil
}

func (s *PageService) GetPage(ctx context.Context, pageType string, id uint64) (*PageView, error) {
	row, err := s.loadPage(ctx, pageType, id)
	if err != nil {
		return nil, err
	}
	return s.pageView(ctx, row)
}

func (s *PageService) Backlinks(ctx context.Context, pageType string, id uint64) ([]Backlink, error) {
	if _, ok := pageTypes[pageType]; !ok && pageType != RefTypeTask && pageType != RefTypeTodo && pageType != RefTypeFact {
		return nil, invalid(fmt.Errorf("page type %q is not supported", pageType))
	}
	if id == 0 {
		return nil, invalid(fmt.Errorf("page id must be positive"))
	}
	links, err := FindBacklinks(ctx, s.db, pageType, id)
	if err != nil {
		return nil, err
	}
	if links == nil {
		return []Backlink{}, nil
	}
	return links, nil
}

func (s *PageService) ListPages(ctx context.Context, filter ListPagesFilter) ([]PageIndexItem, error) {
	if filter.Type != "" {
		if _, ok := pageTypes[filter.Type]; !ok {
			return nil, invalid(fmt.Errorf("page type %q is not supported", filter.Type))
		}
	}
	if filter.StaleDays != nil && *filter.StaleDays < 1 {
		return nil, invalid(fmt.Errorf("stale_days must be a positive integer"))
	}
	types := []string{
		PageTypePrincipal, PageTypePerson, PageTypeProject,
		PageTypeKeyMatter, PageTypeGroup, PageTypeResource,
	}
	if filter.Type != "" {
		types = []string{filter.Type}
	}
	var items []PageIndexItem
	for _, pageType := range types {
		rows, err := s.listType(ctx, pageType, filter)
		if err != nil {
			return nil, err
		}
		items = append(items, rows...)
	}
	if items == nil {
		return []PageIndexItem{}, nil
	}
	return items, nil
}

func (s *PageService) UpdatePage(ctx context.Context, pageType string, id uint64, in UpdatePageInput) (*PageView, error) {
	if _, ok := pageTypes[pageType]; !ok {
		return nil, invalid(fmt.Errorf("page type %q is not supported", pageType))
	}
	if id == 0 {
		return nil, invalid(fmt.Errorf("page id must be positive"))
	}
	if in.IfUnchangedSince.IsZero() {
		return nil, invalid(fmt.Errorf("if_unchanged_since is required"))
	}
	if err := validateSummary(in.Content); err != nil {
		return nil, invalid(err)
	}
	refs := ParseReferences(in.Content)
	if err := ValidateReferences(ctx, s.db, refs); err != nil {
		return nil, invalid(err)
	}
	current, err := s.loadPage(ctx, pageType, id)
	if err != nil {
		return nil, err
	}
	if !sameUpdatedAt(current.UpdatedAt, in.IfUnchangedSince) {
		view, viewErr := s.pageView(ctx, current)
		if viewErr != nil {
			return nil, viewErr
		}
		return nil, &PageConflictError{Current: view}
	}
	oldText := stringValue(current.Summary)
	if oldText != in.Content {
		now := s.now().UTC()
		if oldText != "" {
			revision := domain.PageRevision{
				PageType: pageType, PageID: id, OldText: oldText, ChangedAt: now,
			}
			if err := s.db.WithContext(ctx).Create(&revision).Error; err != nil {
				return nil, fmt.Errorf("archive page revision %s:%d: %w", pageType, id, err)
			}
		}
		updates := map[string]any{"summary": in.Content, "last_progress_at": now}
		if err := s.writeSummary(ctx, pageType, id, updates); err != nil {
			return nil, err
		}
	}
	return s.GetPage(ctx, pageType, id)
}

type pageRow struct {
	Type           string
	ID             uint64
	Name           string
	Summary        *string
	UpdatedAt      time.Time
	LastProgressAt *time.Time
}

func (s *PageService) loadPage(ctx context.Context, pageType string, id uint64) (*pageRow, error) {
	if _, ok := pageTypes[pageType]; !ok {
		return nil, invalid(fmt.Errorf("page type %q is not supported", pageType))
	}
	if id == 0 {
		return nil, invalid(fmt.Errorf("page id must be positive"))
	}
	row := &pageRow{Type: pageType, ID: id}
	var err error
	switch pageType {
	case PageTypeProject:
		var item domain.Project
		err = s.db.WithContext(ctx).Where("id = ?", id).Take(&item).Error
		row.Name, row.Summary, row.UpdatedAt, row.LastProgressAt = item.Name, item.Summary, item.UpdatedAt, item.LastProgressAt
	case PageTypePerson:
		var item domain.Person
		err = s.db.WithContext(ctx).Where("id = ?", id).Take(&item).Error
		row.Name, row.Summary, row.UpdatedAt, row.LastProgressAt = item.Name, item.Summary, item.UpdatedAt, item.LastProgressAt
	case PageTypeKeyMatter:
		var item domain.KeyMatter
		err = s.db.WithContext(ctx).Where("id = ?", id).Take(&item).Error
		row.Name, row.Summary, row.UpdatedAt, row.LastProgressAt = item.Title, item.Summary, item.UpdatedAt, item.LastProgressAt
	case PageTypeGroup:
		var item domain.Group
		err = s.db.WithContext(ctx).Where("id = ?", id).Take(&item).Error
		row.Name = groupDisplayName(&item)
		row.Summary, row.UpdatedAt, row.LastProgressAt = item.Summary, item.UpdatedAt, item.LastProgressAt
	case PageTypeResource:
		var item domain.ManagedResource
		err = s.db.WithContext(ctx).Where("id = ?", id).Take(&item).Error
		row.Name, row.Summary, row.UpdatedAt, row.LastProgressAt = item.Title, item.Summary, item.UpdatedAt, item.LastProgressAt
	case PageTypePrincipal:
		var item domain.PrincipalProfile
		err = s.db.WithContext(ctx).Where("id = ?", id).Take(&item).Error
		row.Name, row.Summary, row.UpdatedAt, row.LastProgressAt = item.Name, item.Summary, item.UpdatedAt, item.LastProgressAt
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get page %s:%d: %w", pageType, id, err)
	}
	return row, nil
}

func (s *PageService) writeSummary(ctx context.Context, pageType string, id uint64, updates map[string]any) error {
	var model any
	switch pageType {
	case PageTypeProject:
		model = &domain.Project{}
	case PageTypePerson:
		model = &domain.Person{}
	case PageTypeKeyMatter:
		model = &domain.KeyMatter{}
	case PageTypeGroup:
		model = &domain.Group{}
	case PageTypeResource:
		model = &domain.ManagedResource{}
	case PageTypePrincipal:
		model = &domain.PrincipalProfile{}
	default:
		return invalid(fmt.Errorf("page type %q is not supported", pageType))
	}
	result := s.db.WithContext(ctx).Model(model).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update page %s:%d: %w", pageType, id, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("update page %s:%d affected %d rows", pageType, id, result.RowsAffected)
	}
	return nil
}

func (s *PageService) pageView(ctx context.Context, row *pageRow) (*PageView, error) {
	summary := stringValue(row.Summary)
	backlinks, err := FindBacklinks(ctx, s.db, row.Type, row.ID)
	if err != nil {
		return nil, err
	}
	if backlinks == nil {
		backlinks = []Backlink{}
	}
	outgoing := ParseReferences(summary)
	if outgoing == nil {
		outgoing = []Reference{}
	}
	var factCount int64
	if err := s.db.WithContext(ctx).Model(&domain.Fact{}).
		Where("subject_type = ? AND subject_id = ?", row.Type, row.ID).
		Count(&factCount).Error; err != nil {
		return nil, fmt.Errorf("count facts for %s:%d: %w", row.Type, row.ID, err)
	}
	return &PageView{
		Type: row.Type, ID: row.ID, Name: row.Name, Summary: summary,
		CharCount: utf8.RuneCountInString(summary), MaxChars: SummaryMaxChars,
		UpdatedAt: row.UpdatedAt, LastProgressAt: row.LastProgressAt,
		Outgoing: outgoing, Backlinks: backlinks, FactCount: factCount,
	}, nil
}

func (s *PageService) listType(ctx context.Context, pageType string, filter ListPagesFilter) ([]PageIndexItem, error) {
	query := s.db.WithContext(ctx)
	switch pageType {
	case PageTypeProject:
		query = query.Model(&domain.Project{})
		if !filter.All {
			query = query.Where("status = ?", "active")
		}
	case PageTypePerson:
		query = query.Model(&domain.Person{})
		if !filter.All {
			query = query.Where("is_active = ? AND role IN ?", true, []string{"leader", "key"})
		}
	case PageTypeKeyMatter:
		query = query.Model(&domain.KeyMatter{})
		if !filter.All {
			query = query.Where("closed_at IS NULL")
		}
	case PageTypeGroup:
		query = query.Model(&domain.Group{})
		if !filter.All {
			query = query.Where("related_group = ? AND tier <> ?", true, "cold")
		}
	case PageTypeResource:
		query = query.Model(&domain.ManagedResource{})
		if !filter.All {
			query = query.Where("is_active = ?", true)
		}
	case PageTypePrincipal:
		query = query.Model(&domain.PrincipalProfile{})
	default:
		return nil, invalid(fmt.Errorf("page type %q is not supported", pageType))
	}
	if filter.StaleDays != nil {
		cutoff := s.now().UTC().Add(-time.Duration(*filter.StaleDays) * 24 * time.Hour)
		query = query.Where("last_progress_at IS NULL OR last_progress_at < ?", cutoff)
	}
	type listRow struct {
		ID             uint64
		Name           string
		Title          string
		ChatID         string
		Summary        *string
		LastProgressAt *time.Time
	}
	var rows []listRow
	if err := query.Select(listSelect(pageType)).Order("id ASC").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list pages type=%s: %w", pageType, err)
	}
	items := make([]PageIndexItem, 0, len(rows))
	for _, row := range rows {
		summary := stringValue(row.Summary)
		n := utf8.RuneCountInString(summary)
		if filter.OverLimit && n <= SummaryMaxChars {
			continue
		}
		name := row.Name
		if pageType == PageTypeKeyMatter {
			name = row.Title
		}
		if pageType == PageTypeGroup && name == "" {
			name = row.ChatID
		}
		if pageType == PageTypeResource {
			name = row.Title
		}
		items = append(items, PageIndexItem{
			Type: pageType, ID: row.ID, Name: name,
			IndexLine: summaryIndexLine(row.Summary), CharCount: n,
			LastProgressAt: row.LastProgressAt,
		})
	}
	return items, nil
}

func listSelect(pageType string) string {
	switch pageType {
	case PageTypeKeyMatter:
		return "id, title, summary, last_progress_at"
	case PageTypeGroup:
		return "id, name, chat_id, summary, last_progress_at"
	case PageTypeResource:
		return "id, title, summary, last_progress_at"
	default:
		return "id, name, summary, last_progress_at"
	}
}

func groupDisplayName(group *domain.Group) string {
	if group.Name != nil && strings.TrimSpace(*group.Name) != "" {
		return *group.Name
	}
	return group.ChatID
}

// sameUpdatedAt compares instants exactly. Both sides originate from the same
// database column, so their precision matches; truncating here would let two
// writers inside one second silently overwrite each other.
func sameUpdatedAt(stored, claimed time.Time) bool {
	return stored.Equal(claimed)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
