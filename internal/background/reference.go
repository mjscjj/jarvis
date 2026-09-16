package background

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

const (
	PageTypePrincipal     = "principal"
	PageTypePerson        = "person"
	PageTypeProject       = "project"
	PageTypeKeyMatter     = "key_matter"
	PageTypeProjectRisk   = "project_risk"
	PageTypeProjectChange = "project_change"
	PageTypeGroup         = "group"
	PageTypeResource      = "resource"
	RefTypeTask           = "task"
	RefTypeTodo           = "todo"
	RefTypeFact           = "fact"
)

var pageTypes = map[string]struct{}{
	PageTypePrincipal: {}, PageTypePerson: {}, PageTypeProject: {},
	PageTypeKeyMatter: {}, PageTypeProjectRisk: {}, PageTypeProjectChange: {},
	PageTypeGroup: {}, PageTypeResource: {},
}

var referenceSchemes = map[string]struct{}{
	PageTypePrincipal: {}, PageTypePerson: {}, PageTypeProject: {},
	PageTypeKeyMatter: {}, PageTypeProjectRisk: {}, PageTypeProjectChange: {},
	PageTypeGroup: {}, PageTypeResource: {},
	RefTypeTask: {}, RefTypeTodo: {}, RefTypeFact: {},
}

var referencePattern = regexp.MustCompile(`\[[^\]\n]*\]\((principal|person|project|key_matter|project_risk|project_change|group|resource|task|todo|fact):(\d+)\)`)

// Reference is one Markdown entity link extracted from page text.
type Reference struct {
	Type string `json:"type"`
	ID   uint64 `json:"id"`
}

// Backlink is one page whose summary mentions the requested entity URI.
type Backlink struct {
	Type      string `json:"type"`
	ID        uint64 `json:"id"`
	Name      string `json:"name"`
	IndexLine string `json:"index_line"`
}

// ParseReferences extracts Markdown links of the form [text](person:12).
func ParseReferences(content string) []Reference {
	matches := referencePattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	refs := make([]Reference, 0, len(matches))
	for _, match := range matches {
		id, err := strconv.ParseUint(match[2], 10, 64)
		if err != nil || id == 0 {
			continue
		}
		key := match[1] + ":" + match[2]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, Reference{Type: match[1], ID: id})
	}
	return refs
}

// ValidateReferences checks that every referenced entity exists. A missing
// target is a hard error: this is the only guard against fabricated IDs.
func ValidateReferences(ctx context.Context, db *gorm.DB, refs []Reference) error {
	if db == nil {
		return fmt.Errorf("validate references: db is nil")
	}
	for _, ref := range refs {
		if _, ok := referenceSchemes[ref.Type]; !ok {
			return fmt.Errorf("reference scheme %q is not supported", ref.Type)
		}
		if ref.ID == 0 {
			return fmt.Errorf("reference %s:0 is invalid", ref.Type)
		}
		exists, err := referenceExists(ctx, db, ref)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("referenced %s:%d does not exist", ref.Type, ref.ID)
		}
	}
	return nil
}

// FindBacklinks returns pages whose summary contains the URI token (person:12).
func FindBacklinks(ctx context.Context, db *gorm.DB, pageType string, id uint64) ([]Backlink, error) {
	if db == nil {
		return nil, fmt.Errorf("find backlinks: db is nil")
	}
	if _, ok := pageTypes[pageType]; !ok && pageType != RefTypeTask && pageType != RefTypeTodo && pageType != RefTypeFact {
		return nil, fmt.Errorf("page type %q is not supported", pageType)
	}
	if id == 0 {
		return nil, fmt.Errorf("backlink id must be positive")
	}
	token := "%(" + pageType + ":" + strconv.FormatUint(id, 10) + ")%"
	var links []Backlink
	type namedSummary struct {
		ID      uint64
		Name    string
		Summary *string
	}
	scan := func(pageType, table, nameExpr string) error {
		var rows []namedSummary
		if err := db.WithContext(ctx).Table(table).
			Select("id, "+nameExpr+" AS name, summary").
			Where("summary LIKE ?", token).
			Order("id ASC").
			Scan(&rows).Error; err != nil {
			return fmt.Errorf("find backlinks in %s: %w", table, err)
		}
		for _, row := range rows {
			links = append(links, Backlink{
				Type: pageType, ID: row.ID, Name: row.Name, IndexLine: summaryIndexLine(row.Summary),
			})
		}
		return nil
	}
	if err := scan(PageTypeProject, "project", "name"); err != nil {
		return nil, err
	}
	if err := scan(PageTypePerson, "person", "name"); err != nil {
		return nil, err
	}
	if err := scan(PageTypeKeyMatter, "key_matter", "title"); err != nil {
		return nil, err
	}
	if err := scan(PageTypeProjectRisk, "project_risk", "title"); err != nil {
		return nil, err
	}
	if err := scan(PageTypeProjectChange, "project_change", "title"); err != nil {
		return nil, err
	}
	if err := scan(PageTypeGroup, "feishu_group", "COALESCE(name, chat_id)"); err != nil {
		return nil, err
	}
	if err := scan(PageTypeResource, "managed_resource", "title"); err != nil {
		return nil, err
	}
	if err := scan(PageTypePrincipal, "principal_profile", "name"); err != nil {
		return nil, err
	}
	return links, nil
}

func referenceExists(ctx context.Context, db *gorm.DB, ref Reference) (bool, error) {
	var model any
	switch ref.Type {
	case PageTypePrincipal:
		model = &domain.PrincipalProfile{}
	case PageTypePerson:
		model = &domain.Person{}
	case PageTypeProject:
		model = &domain.Project{}
	case PageTypeKeyMatter:
		model = &domain.KeyMatter{}
	case PageTypeProjectRisk:
		model = &domain.ProjectRisk{}
	case PageTypeProjectChange:
		model = &domain.ProjectChange{}
	case PageTypeGroup:
		model = &domain.Group{}
	case PageTypeResource:
		model = &domain.ManagedResource{}
	case RefTypeTask:
		model = &domain.Task{}
	case RefTypeTodo:
		model = &domain.Todo{}
	case RefTypeFact:
		model = &domain.Fact{}
	default:
		return false, fmt.Errorf("reference scheme %q is not supported", ref.Type)
	}
	var count int64
	if err := db.WithContext(ctx).Model(model).Where("id = ?", ref.ID).Count(&count).Error; err != nil {
		return false, fmt.Errorf("lookup referenced %s:%d: %w", ref.Type, ref.ID, err)
	}
	return count == 1, nil
}

func summaryIndexLine(summary *string) string {
	if summary == nil {
		return ""
	}
	text := *summary
	if i := indexNewline(text); i >= 0 {
		return text[:i]
	}
	return text
}

func indexNewline(text string) int {
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			return i
		}
	}
	return -1
}
