package background

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	Query     string
	Cursor    string
	PageSize  int
}

type PageIndexPage struct {
	Items      []PageIndexItem `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type PageRevisionPage struct {
	Items      []domain.PageRevision `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
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

// PageService reads and writes the world entity summary pages.
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

// ListRevisions returns complete archived page texts newest first. Revisions
// are read-only evidence about our own notes; restoring one remains an Agent
// decision made through the ordinary CAS update tool.
func (s *PageService) ListRevisions(ctx context.Context, pageType string, id, cursor uint64, limit int) (*PageRevisionPage, error) {
	if _, ok := pageTypes[pageType]; !ok {
		return nil, invalid(fmt.Errorf("page type %q is not supported", pageType))
	}
	if id == 0 {
		return nil, invalid(fmt.Errorf("page id must be positive"))
	}
	if limit <= 0 || limit > 100 {
		return nil, invalid(fmt.Errorf("limit must be between 1 and 100"))
	}
	if _, err := s.loadPage(ctx, pageType, id); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Where("page_type = ? AND page_id = ?", pageType, id)
	if cursor != 0 {
		query = query.Where("id < ?", cursor)
	}
	var rows []domain.PageRevision
	if err := query.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list page revisions %s:%d: %w", pageType, id, err)
	}
	page := &PageRevisionPage{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		page.NextCursor = strconv.FormatUint(page.Items[len(page.Items)-1].ID, 10)
	}
	if page.Items == nil {
		page.Items = []domain.PageRevision{}
	}
	return page, nil
}

func (s *PageService) ListPages(ctx context.Context, filter ListPagesFilter) ([]PageIndexItem, error) {
	filter.Cursor = ""
	filter.PageSize = 200
	items := []PageIndexItem{}
	for {
		page, err := s.ListPagesPage(ctx, filter)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextCursor == "" {
			return items, nil
		}
		filter.Cursor = page.NextCursor
	}
}

func (s *PageService) ListPagesPage(ctx context.Context, filter ListPagesFilter) (*PageIndexPage, error) {
	if filter.Type != "" {
		if _, ok := pageTypes[filter.Type]; !ok {
			return nil, invalid(fmt.Errorf("page type %q is not supported", filter.Type))
		}
	}
	if filter.StaleDays != nil && *filter.StaleDays < 1 {
		return nil, invalid(fmt.Errorf("stale_days must be a positive integer"))
	}
	if filter.PageSize <= 0 || filter.PageSize > 200 {
		return nil, invalid(fmt.Errorf("page_size must be between 1 and 200"))
	}
	filter.Query = strings.TrimSpace(filter.Query)
	types := []string{
		PageTypePrincipal, PageTypePerson, PageTypeProject,
		PageTypeKeyMatter, PageTypeGroup, PageTypeResource,
	}
	if filter.Type != "" {
		types = []string{filter.Type}
	}
	cursorType, cursorID, err := parsePageCursor(filter.Cursor, types)
	if err != nil {
		return nil, invalid(err)
	}
	items := make([]PageIndexItem, 0, filter.PageSize+1)
	started := cursorType == ""
	for _, pageType := range types {
		if !started {
			if pageType != cursorType {
				continue
			}
			started = true
		}
		afterID := uint64(0)
		if pageType == cursorType {
			afterID = cursorID
		}
		rows, err := s.listType(ctx, pageType, filter, afterID, filter.PageSize+1-len(items))
		if err != nil {
			return nil, err
		}
		items = append(items, rows...)
		if len(items) > filter.PageSize {
			break
		}
	}
	page := &PageIndexPage{Items: items}
	if len(items) > filter.PageSize {
		page.Items = items[:filter.PageSize]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = fmt.Sprintf("%s:%d", last.Type, last.ID)
	}
	return page, nil
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
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txService := &PageService{db: tx, now: s.now}
		current, err := txService.loadPage(ctx, pageType, id)
		if err != nil {
			return err
		}
		if !sameUpdatedAt(current.UpdatedAt, in.IfUnchangedSince) {
			view, viewErr := txService.pageView(ctx, current)
			if viewErr != nil {
				return viewErr
			}
			return &PageConflictError{Current: view}
		}
		oldText := stringValue(current.Summary)
		if oldText == in.Content {
			return nil
		}
		expectedUpdatedAt, err := txService.storedUpdatedAtText(ctx, pageType, id)
		if err != nil {
			return err
		}

		now := s.now().UTC()
		if oldText != "" {
			revision := domain.PageRevision{
				PageType: pageType, PageID: id, OldText: oldText, ChangedAt: now,
			}
			if err := tx.WithContext(ctx).Create(&revision).Error; err != nil {
				return fmt.Errorf("archive page revision %s:%d: %w", pageType, id, err)
			}
		}
		updates := map[string]any{"summary": in.Content, "last_progress_at": now}
		return txService.writeSummary(ctx, pageType, id, expectedUpdatedAt, updates)
	})
	if err != nil {
		return nil, err
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

func (s *PageService) writeSummary(ctx context.Context, pageType string, id uint64, expectedUpdatedAt string, updates map[string]any) error {
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
	result := s.db.WithContext(ctx).Model(model).
		Where("id = ? AND CAST(updated_at AS TEXT) = ?", id, expectedUpdatedAt).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update page %s:%d: %w", pageType, id, result.Error)
	}
	if result.RowsAffected == 0 {
		current, err := s.loadPage(ctx, pageType, id)
		if err != nil {
			return err
		}
		view, err := s.pageView(ctx, current)
		if err != nil {
			return err
		}
		return &PageConflictError{Current: view}
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("update page %s:%d affected %d rows", pageType, id, result.RowsAffected)
	}
	return nil
}

func (s *PageService) storedUpdatedAtText(ctx context.Context, pageType string, id uint64) (string, error) {
	table := map[string]string{
		PageTypeProject: "project", PageTypePerson: "person",
		PageTypeKeyMatter: "key_matter", PageTypeGroup: "feishu_group",
		PageTypeResource: "managed_resource", PageTypePrincipal: "principal_profile",
	}[pageType]
	if table == "" {
		return "", invalid(fmt.Errorf("page type %q is not supported", pageType))
	}
	var value string
	if err := s.db.WithContext(ctx).Raw(
		"SELECT CAST(updated_at AS TEXT) FROM `"+table+"` WHERE id = ?", id,
	).Scan(&value).Error; err != nil {
		return "", fmt.Errorf("load page version %s:%d: %w", pageType, id, err)
	}
	if value == "" {
		return "", ErrNotFound
	}
	return value, nil
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
	subjectTypes := []string{row.Type}
	if row.Type == PageTypeResource {
		subjectTypes = append(subjectTypes, "managed_resource")
	}
	var factCount int64
	if err := s.db.WithContext(ctx).Model(&domain.Fact{}).
		Where("subject_type IN ? AND subject_id = ?", subjectTypes, row.ID).
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

func (s *PageService) listType(ctx context.Context, pageType string, filter ListPagesFilter, afterID uint64, limit int) ([]PageIndexItem, error) {
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
	if afterID != 0 {
		query = query.Where("id > ?", afterID)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		switch pageType {
		case PageTypeKeyMatter, PageTypeResource:
			query = query.Where("title LIKE ? OR summary LIKE ?", like, like)
		case PageTypeGroup:
			query = query.Where("name LIKE ? OR chat_id LIKE ? OR summary LIKE ?", like, like, like)
		default:
			query = query.Where("name LIKE ? OR summary LIKE ?", like, like)
		}
	}
	if filter.OverLimit {
		query = query.Where("length(COALESCE(summary, '')) > ?", SummaryMaxChars)
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
	if err := query.Select(listSelect(pageType)).Order("id ASC").Limit(limit).Scan(&rows).Error; err != nil {
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

func parsePageCursor(cursor string, types []string) (string, uint64, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return "", 0, nil
	}
	pageType, rawID, ok := strings.Cut(cursor, ":")
	if !ok {
		return "", 0, fmt.Errorf("cursor is invalid")
	}
	id, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil || id == 0 {
		return "", 0, fmt.Errorf("cursor is invalid")
	}
	for _, candidate := range types {
		if candidate == pageType {
			return pageType, id, nil
		}
	}
	return "", 0, fmt.Errorf("cursor does not belong to the selected page types")
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
