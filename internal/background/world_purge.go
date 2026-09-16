package background

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// WorldPurgeEntity identifies one disposable world entity by both its stable
// database ID and its current human-readable name. Requiring both prevents a
// stale cleanup manifest from deleting a row whose ID has been reused or whose
// meaning has changed since the manifest was reviewed.
type WorldPurgeEntity struct {
	Type         string `json:"type"`
	ID           uint64 `json:"id"`
	ExpectedName string `json:"expected_name"`
}

type WorldPurgeInput struct {
	Entities   []WorldPurgeEntity    `json:"entities"`
	PageBlocks []WorldPurgePageBlock `json:"page_blocks"`
}

// WorldPurgePageBlock removes one explicitly delimited generated block from a
// preserved entity page. Marker is the text between the standard HTML comment
// prefix and :start/:end suffixes.
type WorldPurgePageBlock struct {
	Type         string `json:"type"`
	ID           uint64 `json:"id"`
	ExpectedName string `json:"expected_name"`
	Marker       string `json:"marker"`
}

type WorldPurgeResult struct {
	Entities      map[string]int64 `json:"entities"`
	Relations     int64            `json:"relations"`
	Facts         int64            `json:"facts"`
	PageRevisions int64            `json:"page_revisions"`
	PageBlocks    int64            `json:"page_blocks"`
}

// WorldPurgeService physically removes explicitly named disposable world
// entities and the records owned by their identity. Normal product deletion
// remains archive/close; this service is intentionally narrow and refuses to
// sever live business bindings or preserved-page backlinks.
type WorldPurgeService struct{ db *gorm.DB }

func NewWorldPurgeService(db *gorm.DB) (*WorldPurgeService, error) {
	if db == nil {
		return nil, fmt.Errorf("world purge service db is nil")
	}
	return &WorldPurgeService{db: db}, nil
}

type worldPurgeSet map[string]map[uint64]struct{}
type worldPurgePageKey struct {
	Type string
	ID   uint64
}

func (s *WorldPurgeService) Purge(ctx context.Context, input WorldPurgeInput) (*WorldPurgeResult, error) {
	set, cleanedPages, err := s.validateManifest(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := s.validateDependencies(ctx, set, cleanedPages); err != nil {
		return nil, err
	}
	result := &WorldPurgeResult{Entities: map[string]int64{
		PageTypeProject: 0, PageTypeKeyMatter: 0, PageTypePerson: 0,
	}}
	// The repository deliberately avoids multi-step database transactions. All
	// identities, block shapes and live dependencies are validated before this
	// point, then writes remain ordered and fail fast.
	if err := s.stripPageBlocks(ctx, input.PageBlocks, result); err != nil {
		return nil, err
	}

	for _, entityType := range []string{PageTypeKeyMatter, PageTypeProject, PageTypePerson} {
		for id := range set[entityType] {
			nodeID := strconv.FormatUint(id, 10)
			deleted, err := s.deleteWhere(ctx, &domain.EntityRelation{},
				"(source_type = ? AND source_id = ?) OR (target_type = ? AND target_id = ?)",
				entityType, nodeID, entityType, nodeID)
			if err != nil {
				return nil, fmt.Errorf("purge relations for %s:%d: %w", entityType, id, err)
			}
			result.Relations += deleted
		}
	}
	for _, entityType := range []string{PageTypeKeyMatter, PageTypeProject, PageTypePerson} {
		for id := range set[entityType] {
			deleted, err := s.deleteWhere(ctx, &domain.Fact{}, "subject_type = ? AND subject_id = ?", entityType, id)
			if err != nil {
				return nil, fmt.Errorf("purge facts for %s:%d: %w", entityType, id, err)
			}
			result.Facts += deleted
			deleted, err = s.deleteWhere(ctx, &domain.PageRevision{}, "page_type = ? AND page_id = ?", entityType, id)
			if err != nil {
				return nil, fmt.Errorf("purge page revisions for %s:%d: %w", entityType, id, err)
			}
			result.PageRevisions += deleted
		}
	}
	for _, entityType := range []string{PageTypeKeyMatter, PageTypeProject, PageTypePerson} {
		for id := range set[entityType] {
			var model any
			switch entityType {
			case PageTypeProject:
				model = &domain.Project{}
			case PageTypeKeyMatter:
				model = &domain.KeyMatter{}
			case PageTypePerson:
				model = &domain.Person{}
			}
			deleted, err := s.deleteWhere(ctx, model, "id = ?", id)
			if err != nil {
				return nil, fmt.Errorf("purge %s:%d: %w", entityType, id, err)
			}
			if deleted != 1 {
				return nil, fmt.Errorf("purge %s:%d affected %d rows", entityType, id, deleted)
			}
			result.Entities[entityType]++
		}
	}
	return result, nil
}

func (s *WorldPurgeService) validateManifest(ctx context.Context, input WorldPurgeInput) (worldPurgeSet, map[worldPurgePageKey]string, error) {
	if len(input.Entities) == 0 && len(input.PageBlocks) == 0 {
		return nil, nil, invalid(fmt.Errorf("entities and page_blocks must not both be empty"))
	}
	set := worldPurgeSet{PageTypeProject: {}, PageTypeKeyMatter: {}, PageTypePerson: {}}
	for _, entity := range input.Entities {
		entity.Type = strings.TrimSpace(entity.Type)
		if _, ok := set[entity.Type]; !ok {
			return nil, nil, invalid(fmt.Errorf("entity type %q cannot be purged", entity.Type))
		}
		if entity.ID == 0 || strings.TrimSpace(entity.ExpectedName) == "" {
			return nil, nil, invalid(fmt.Errorf("entity %s:%d requires a positive id and expected_name", entity.Type, entity.ID))
		}
		if _, exists := set[entity.Type][entity.ID]; exists {
			return nil, nil, invalid(fmt.Errorf("entity %s:%d is duplicated", entity.Type, entity.ID))
		}
		actual, err := s.entityName(ctx, entity.Type, entity.ID)
		if err != nil {
			return nil, nil, err
		}
		if actual != entity.ExpectedName {
			return nil, nil, invalid(fmt.Errorf("entity %s:%d name is %q, expected %q", entity.Type, entity.ID, actual, entity.ExpectedName))
		}
		set[entity.Type][entity.ID] = struct{}{}
	}
	seenBlocks := make(map[string]struct{}, len(input.PageBlocks))
	cleanedPages := make(map[worldPurgePageKey]string)
	for _, block := range input.PageBlocks {
		block.Type = strings.TrimSpace(block.Type)
		block.Marker = strings.TrimSpace(block.Marker)
		if _, ok := pageTypes[block.Type]; !ok {
			return nil, nil, invalid(fmt.Errorf("page block type %q is not supported", block.Type))
		}
		if block.ID == 0 || strings.TrimSpace(block.ExpectedName) == "" || block.Marker == "" {
			return nil, nil, invalid(fmt.Errorf("page block %s:%d requires a positive id, expected_name and marker", block.Type, block.ID))
		}
		key := fmt.Sprintf("%s:%d:%s", block.Type, block.ID, block.Marker)
		if _, exists := seenBlocks[key]; exists {
			return nil, nil, invalid(fmt.Errorf("page block %s is duplicated", key))
		}
		seenBlocks[key] = struct{}{}
		row, err := s.loadPurgePage(ctx, block.Type, block.ID)
		if err != nil {
			return nil, nil, err
		}
		if row.Name != block.ExpectedName {
			return nil, nil, invalid(fmt.Errorf("page %s:%d name is %q, expected %q", block.Type, block.ID, row.Name, block.ExpectedName))
		}
		pageKey := worldPurgePageKey{Type: block.Type, ID: block.ID}
		summary, exists := cleanedPages[pageKey]
		if !exists {
			summary = row.Summary
		}
		cleaned, err := removeGeneratedBlock(summary, block.Marker)
		if err != nil {
			return nil, nil, invalid(fmt.Errorf("page %s:%d: %w", block.Type, block.ID, err))
		}
		cleanedPages[pageKey] = cleaned
	}
	return set, cleanedPages, nil
}

func (s *WorldPurgeService) stripPageBlocks(ctx context.Context, blocks []WorldPurgePageBlock, result *WorldPurgeResult) error {
	for _, block := range blocks {
		row, err := s.loadPurgePage(ctx, block.Type, block.ID)
		if err != nil {
			return err
		}
		cleaned, err := removeGeneratedBlock(row.Summary, block.Marker)
		if err != nil {
			return invalid(fmt.Errorf("page %s:%d: %w", block.Type, block.ID, err))
		}
		resultWrite := s.db.WithContext(ctx).Table(row.Table).Where("id = ?", block.ID).Updates(map[string]any{
			"summary": cleaned, "last_progress_at": time.Now().UTC(),
		})
		if resultWrite.Error != nil {
			return fmt.Errorf("strip generated block from %s:%d: %w", block.Type, block.ID, resultWrite.Error)
		}
		if resultWrite.RowsAffected != 1 {
			return fmt.Errorf("strip generated block from %s:%d affected %d rows", block.Type, block.ID, resultWrite.RowsAffected)
		}
		markerLike := "%<!-- " + block.Marker + ":start -->%"
		deleted, err := s.deleteWhere(ctx, &domain.PageRevision{},
			"page_type = ? AND page_id = ? AND old_text LIKE ?", block.Type, block.ID, markerLike)
		if err != nil {
			return fmt.Errorf("purge generated page revisions for %s:%d: %w", block.Type, block.ID, err)
		}
		result.PageRevisions += deleted
		result.PageBlocks++
	}
	return nil
}

type worldPurgePage struct {
	Table   string
	Name    string
	Summary string
}

func (s *WorldPurgeService) loadPurgePage(ctx context.Context, pageType string, id uint64) (*worldPurgePage, error) {
	var table, nameColumn string
	switch pageType {
	case PageTypeProject:
		table, nameColumn = "project", "name"
	case PageTypeKeyMatter:
		table, nameColumn = "key_matter", "title"
	case PageTypeProjectRisk:
		table, nameColumn = "project_risk", "title"
	case PageTypeProjectChange:
		table, nameColumn = "project_change", "title"
	case PageTypePerson:
		table, nameColumn = "person", "name"
	case PageTypeGroup:
		table, nameColumn = "feishu_group", "COALESCE(name, chat_id)"
	case PageTypeResource:
		table, nameColumn = "managed_resource", "title"
	case PageTypePrincipal:
		table, nameColumn = "principal_profile", "name"
	default:
		return nil, invalid(fmt.Errorf("page type %q is not supported", pageType))
	}
	row := &worldPurgePage{Table: table}
	query := s.db.WithContext(ctx).Table(table).Select(nameColumn+" AS name, COALESCE(summary, '') AS summary").Where("id = ?", id).Take(row)
	if query.Error != nil {
		if query.Error == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load page %s:%d for purge: %w", pageType, id, query.Error)
	}
	return row, nil
}

func removeGeneratedBlock(summary, marker string) (string, error) {
	start := "<!-- " + marker + ":start -->"
	end := "<!-- " + marker + ":end -->"
	if strings.Count(summary, start) != 1 || strings.Count(summary, end) != 1 {
		return "", fmt.Errorf("expected exactly one %q block", marker)
	}
	startAt := strings.Index(summary, start)
	endAt := strings.Index(summary, end)
	if endAt < startAt {
		return "", fmt.Errorf("marker %q ends before it starts", marker)
	}
	endAt += len(end)
	before := strings.TrimSpace(summary[:startAt])
	after := strings.TrimSpace(summary[endAt:])
	switch {
	case before == "":
		return after, nil
	case after == "":
		return before, nil
	default:
		return before + "\n\n" + after, nil
	}
}

func (s *WorldPurgeService) entityName(ctx context.Context, entityType string, id uint64) (string, error) {
	var name string
	var table, column string
	switch entityType {
	case PageTypeProject:
		table, column = "project", "name"
	case PageTypeKeyMatter:
		table, column = "key_matter", "title"
	case PageTypePerson:
		table, column = "person", "name"
	default:
		return "", invalid(fmt.Errorf("entity type %q cannot be purged", entityType))
	}
	err := s.db.WithContext(ctx).Table(table).Select(column).Where("id = ?", id).Scan(&name).Error
	if err != nil {
		return "", fmt.Errorf("load %s:%d name: %w", entityType, id, err)
	}
	if name == "" {
		return "", ErrNotFound
	}
	return name, nil
}

func (s *WorldPurgeService) validateDependencies(ctx context.Context, set worldPurgeSet, cleanedPages map[worldPurgePageKey]string) error {
	for entityType, ids := range set {
		for id := range ids {
			backlinks, err := FindBacklinks(ctx, s.db, entityType, id)
			if err != nil {
				return err
			}
			for _, backlink := range backlinks {
				if _, deleting := set[backlink.Type][backlink.ID]; !deleting {
					if cleaned, ok := cleanedPages[worldPurgePageKey{Type: backlink.Type, ID: backlink.ID}]; ok &&
						!strings.Contains(cleaned, "("+entityType+":"+strconv.FormatUint(id, 10)+")") {
						continue
					}
					return invalid(fmt.Errorf("%s:%d is referenced by preserved page %s:%d", entityType, id, backlink.Type, backlink.ID))
				}
			}
			if err := s.rejectPreservedRelation(ctx, set, entityType, id); err != nil {
				return err
			}
			if err := s.rejectWorldProgress(ctx, entityType, id); err != nil {
				return err
			}
			if err := s.rejectScheduledTask(ctx, entityType, id); err != nil {
				return err
			}
		}
	}
	for id := range set[PageTypeProject] {
		if err := s.rejectProjectBindings(ctx, set, id); err != nil {
			return err
		}
	}
	for id := range set[PageTypePerson] {
		if err := s.rejectCount(ctx, "managed_resource", "person_id = ?", id, fmt.Sprintf("person:%d has managed resources", id)); err != nil {
			return err
		}
	}
	return nil
}

func (s *WorldPurgeService) rejectProjectBindings(ctx context.Context, set worldPurgeSet, id uint64) error {
	var keyMatters []uint64
	if err := s.db.WithContext(ctx).Table("key_matter").Where("project_id = ?", id).Pluck("id", &keyMatters).Error; err != nil {
		return fmt.Errorf("inspect key matters for project:%d: %w", id, err)
	}
	for _, keyMatterID := range keyMatters {
		if _, deleting := set[PageTypeKeyMatter][keyMatterID]; !deleting {
			return invalid(fmt.Errorf("project:%d is referenced by preserved key_matter:%d", id, keyMatterID))
		}
	}
	for _, binding := range []struct {
		table string
		label string
	}{
		{"project_risk", "project risks"}, {"project_change", "project changes"},
		{"feishu_group", "groups"}, {"managed_resource", "managed resources"},
		{"todo", "todos"}, {"task", "tasks"},
	} {
		if err := s.rejectCount(ctx, binding.table, "project_id = ?", id, fmt.Sprintf("project:%d has %s", id, binding.label)); err != nil {
			return err
		}
	}
	return nil
}

func (s *WorldPurgeService) rejectPreservedRelation(ctx context.Context, set worldPurgeSet, entityType string, id uint64) error {
	nodeID := strconv.FormatUint(id, 10)
	var rows []domain.EntityRelation
	if err := s.db.WithContext(ctx).Where(
		"(source_type = ? AND source_id = ?) OR (target_type = ? AND target_id = ?)",
		entityType, nodeID, entityType, nodeID,
	).Find(&rows).Error; err != nil {
		return fmt.Errorf("inspect relations for %s:%d: %w", entityType, id, err)
	}
	for _, row := range rows {
		otherType, otherID := row.TargetType, row.TargetID
		if row.TargetType == entityType && row.TargetID == nodeID {
			otherType, otherID = row.SourceType, row.SourceID
		}
		if strings.HasPrefix(otherType, "okr_") {
			continue
		}
		parsedID, err := strconv.ParseUint(otherID, 10, 64)
		if err == nil {
			if _, deleting := set[otherType][parsedID]; deleting {
				continue
			}
		}
		return invalid(fmt.Errorf("%s:%d has preserved relation %d to %s:%s", entityType, id, row.ID, otherType, otherID))
	}
	return nil
}

func (s *WorldPurgeService) rejectWorldProgress(ctx context.Context, entityType string, id uint64) error {
	return s.rejectCount(ctx, "world_progress", "subject_type = ? AND subject_id = ?",
		[]any{entityType, strconv.FormatUint(id, 10)}, fmt.Sprintf("%s:%d has world progress", entityType, id))
}

func (s *WorldPurgeService) rejectScheduledTask(ctx context.Context, entityType string, id uint64) error {
	return s.rejectCount(ctx, "scheduled_task", "subject_type = ? AND subject_id = ?",
		[]any{entityType, id}, fmt.Sprintf("%s:%d has scheduled tasks", entityType, id))
}

func (s *WorldPurgeService) rejectCount(ctx context.Context, table, where string, argsOrID any, message string) error {
	args := []any{argsOrID}
	if values, ok := argsOrID.([]any); ok {
		args = values
	}
	var count int64
	if err := s.db.WithContext(ctx).Table(table).Where(where, args...).Count(&count).Error; err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	if count != 0 {
		return invalid(fmt.Errorf("%s (%d)", message, count))
	}
	return nil
}

func (s *WorldPurgeService) deleteWhere(ctx context.Context, model any, where string, args ...any) (int64, error) {
	result := s.db.WithContext(ctx).Where(where, args...).Delete(model)
	return result.RowsAffected, result.Error
}
