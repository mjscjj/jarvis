package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// QueryResourcesName is the function name exposed to the model.
const QueryResourcesName = "query_resources"

// resourceDescriptionRuneCap bounds each returned description so one call cannot
// flood the model context.
const resourceDescriptionRuneCap = 600

// QueryResourcesTool lets the model pull manually curated references (docs,
// links, repos, notes) that the owner maintains in the background, filtered by
// the linked project, person (open_id), or the principal ("me"). It reads the
// human-owned managed_resource table, never the message-derived resource table.
type QueryResourcesTool struct {
	db       *gorm.DB
	timeout  time.Duration
	maxLimit int
}

// NewQueryResourcesTool builds the tool. maxLimit caps how many rows one call
// may return; timeout bounds the single DB query. Arguments are validated
// fail-fast.
func NewQueryResourcesTool(db *gorm.DB, timeout time.Duration, maxLimit int) (*QueryResourcesTool, error) {
	if db == nil {
		return nil, fmt.Errorf("query_resources db is nil")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("query_resources timeout must be positive")
	}
	if maxLimit <= 0 {
		return nil, fmt.Errorf("query_resources max limit must be positive")
	}
	return &QueryResourcesTool{db: db, timeout: timeout, maxLimit: maxLimit}, nil
}

func (t *QueryResourcesTool) Name() string { return QueryResourcesName }

func (t *QueryResourcesTool) Description() string {
	return "查询「我」在后台手动维护的资源（文档/链接/仓库/笔记），可按关联项目(project_id)、关联人(person_open_id)、" +
		"或是否关联「我」(principal_only)过滤，也支持关键词(keyword，对名称与说明做子串匹配)。" +
		"用于补充行动线索所需的背景资料，只返回已启用的资源。"
}

func (t *QueryResourcesTool) Schema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"project_id", "person_open_id", "principal_only", "keyword", "limit"},
		"properties": map[string]any{
			"project_id": map[string]any{
				"type":        []string{"integer", "null"},
				"description": "按关联项目的内部 id 过滤；null 表示不按项目过滤。",
			},
			"person_open_id": map[string]any{
				"type":        []string{"string", "null"},
				"description": "按关联人的飞书 open_id 过滤；null 表示不按人过滤。",
			},
			"principal_only": map[string]any{
				"type":        []string{"boolean", "null"},
				"description": "为 true 时只返回关联「我」(principal)的资源；null 或 false 表示不加此约束。",
			},
			"keyword": map[string]any{
				"type":        []string{"string", "null"},
				"description": "对资源名称与说明做子串匹配的关键词；null 表示不按关键词过滤。",
			},
			"limit": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": fmt.Sprintf("最多返回的资源数，服务端上限 %d。", t.maxLimit),
			},
		},
	}
}

type queryResourcesArgs struct {
	ProjectID     *uint64 `json:"project_id"`
	PersonOpenID  *string `json:"person_open_id"`
	PrincipalOnly *bool   `json:"principal_only"`
	Keyword       *string `json:"keyword"`
	Limit         int     `json:"limit"`
}

type resourceItem struct {
	ID           uint64 `json:"id"`
	Title        string `json:"title"`
	ResourceType string `json:"resource_type"`
	URL          string `json:"url,omitempty"`
	Description  string `json:"description,omitempty"`
	ProjectID    uint64 `json:"project_id,omitempty"`
	PersonID     uint64 `json:"person_id,omitempty"`
	LastActiveAt string `json:"last_active_at"`
}

type queryResourcesResult struct {
	Count     int            `json:"count"`
	Resources []resourceItem `json:"resources"`
}

func (t *QueryResourcesTool) Invoke(ctx context.Context, arguments json.RawMessage) (json.RawMessage, error) {
	args, err := decodeToolArgs[queryResourcesArgs](arguments)
	if err != nil {
		return nil, err
	}
	if args.Limit <= 0 {
		return nil, fmt.Errorf("query_resources limit must be positive")
	}
	limit := args.Limit
	if limit > t.maxLimit {
		limit = t.maxLimit
	}

	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Resolve the person open_id to an internal id before touching the resource
	// table so a non-existent person yields an empty result rather than an error.
	var personID *uint64
	if args.PersonOpenID != nil && strings.TrimSpace(*args.PersonOpenID) != "" {
		var person domain.Person
		err := t.db.WithContext(callCtx).Select("id").Where("open_id = ?", strings.TrimSpace(*args.PersonOpenID)).Take(&person).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				empty := queryResourcesResult{Count: 0, Resources: []resourceItem{}}
				return json.Marshal(empty)
			}
			return nil, fmt.Errorf("query_resources resolve person open_id=%q: %w", *args.PersonOpenID, err)
		}
		personID = &person.ID
	}

	query := t.db.WithContext(callCtx).Model(&domain.ManagedResource{}).Where("is_active = ?", true)
	if args.ProjectID != nil {
		query = query.Where("project_id = ?", *args.ProjectID)
	}
	if personID != nil {
		query = query.Where("person_id = ?", *personID)
	}
	if args.PrincipalOnly != nil && *args.PrincipalOnly {
		query = query.Where("link_principal = ?", true)
	}
	if args.Keyword != nil && strings.TrimSpace(*args.Keyword) != "" {
		like := "%" + likeEscape(strings.TrimSpace(*args.Keyword)) + "%"
		query = query.Where("title LIKE ? OR description LIKE ?", like, like)
	}

	var rows []domain.ManagedResource
	if err := query.Order("datetime(last_active_at) DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query_resources load resources: %w", err)
	}

	result := queryResourcesResult{Count: len(rows), Resources: make([]resourceItem, len(rows))}
	for i := range rows {
		item := resourceItem{
			ID: rows[i].ID, Title: rows[i].Title, ResourceType: rows[i].ResourceType,
			LastActiveAt: rows[i].LastActiveAt.UTC().Format(time.RFC3339),
		}
		if rows[i].URL != nil {
			item.URL = *rows[i].URL
		}
		if rows[i].Description != nil {
			item.Description = capRunes(*rows[i].Description, resourceDescriptionRuneCap)
		}
		if rows[i].ProjectID != nil {
			item.ProjectID = *rows[i].ProjectID
		}
		if rows[i].PersonID != nil {
			item.PersonID = *rows[i].PersonID
		}
		result.Resources[i] = item
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("query_resources encode result: %w", err)
	}
	return encoded, nil
}
