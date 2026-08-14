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

// QueryChatHistoryName is the function name exposed to the model.
const QueryChatHistoryName = "query_chat_history"

// historyContentRuneCap bounds each returned message body so a single call
// cannot flood the model context with long messages.
const historyContentRuneCap = 800

// QueryChatHistoryTool lets the model pull older messages of a chat on demand,
// from the local message table (captured by M2), instead of everything being
// pre-stuffed into the prompt. It reads the same source of truth the extraction
// unit is built from, so quotes stay verifiable.
type QueryChatHistoryTool struct {
	db       *gorm.DB
	timeout  time.Duration
	maxLimit int
	location *time.Location
}

// NewQueryChatHistoryTool builds the tool. maxLimit caps how many rows one call
// may return (defense against context flooding); timeout bounds the single DB
// query. All arguments are validated fail-fast.
func NewQueryChatHistoryTool(db *gorm.DB, timeout time.Duration, maxLimit int, location *time.Location) (*QueryChatHistoryTool, error) {
	if db == nil {
		return nil, fmt.Errorf("query_chat_history db is nil")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("query_chat_history timeout must be positive")
	}
	if maxLimit <= 0 {
		return nil, fmt.Errorf("query_chat_history max limit must be positive")
	}
	if location == nil {
		return nil, fmt.Errorf("query_chat_history location is nil")
	}
	return &QueryChatHistoryTool{db: db, timeout: timeout, maxLimit: maxLimit, location: location}, nil
}

func (t *QueryChatHistoryTool) Name() string { return QueryChatHistoryName }

func (t *QueryChatHistoryTool) Description() string {
	return "查询某个飞书群(chat_id)的历史消息，用于推断行动线索的归属项目、代码仓库、分支或历史背景。" +
		"支持按时间范围(start_time/end_time, RFC3339)和关键词(keyword, 对消息正文做子串匹配)过滤，" +
		"按发送时间升序返回。仅用于背景推断，不得把返回的历史消息当作新的 [new] 证据。"
}

func (t *QueryChatHistoryTool) Schema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"chat_id", "start_time", "end_time", "keyword", "limit"},
		"properties": map[string]any{
			"chat_id": map[string]any{
				"type":        "string",
				"description": "要查询的飞书群 chat_id。",
			},
			"start_time": map[string]any{
				"type":        []string{"string", "null"},
				"description": "起始时间(含)，RFC3339 格式，如 2026-01-01T00:00:00+08:00；null 表示不限下界。",
			},
			"end_time": map[string]any{
				"type":        []string{"string", "null"},
				"description": "结束时间(含)，RFC3339 格式；null 表示不限上界。",
			},
			"keyword": map[string]any{
				"type":        []string{"string", "null"},
				"description": "对消息正文做子串匹配的关键词；null 表示不按关键词过滤。",
			},
			"limit": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": fmt.Sprintf("最多返回的消息数，服务端上限 %d。", t.maxLimit),
			},
		},
	}
}

type queryChatHistoryArgs struct {
	ChatID    string  `json:"chat_id"`
	StartTime *string `json:"start_time"`
	EndTime   *string `json:"end_time"`
	Keyword   *string `json:"keyword"`
	Limit     int     `json:"limit"`
}

type historyMessage struct {
	MessageID  string `json:"message_id"`
	SenderName string `json:"sender_name"`
	Time       string `json:"time"`
	Content    string `json:"content"`
}

type queryChatHistoryResult struct {
	ChatID   string           `json:"chat_id"`
	Count    int              `json:"count"`
	Messages []historyMessage `json:"messages"`
}

func (t *QueryChatHistoryTool) Invoke(ctx context.Context, arguments json.RawMessage) (json.RawMessage, error) {
	args, err := decodeToolArgs[queryChatHistoryArgs](arguments)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.ChatID) == "" {
		return nil, fmt.Errorf("query_chat_history chat_id must be non-blank")
	}
	if args.Limit <= 0 {
		return nil, fmt.Errorf("query_chat_history limit must be positive")
	}
	limit := args.Limit
	if limit > t.maxLimit {
		limit = t.maxLimit
	}
	// Parse time bounds before touching the DB so malformed arguments fail fast
	// without an I/O round-trip.
	var startMS, endMS *int64
	if args.StartTime != nil && strings.TrimSpace(*args.StartTime) != "" {
		start, err := time.Parse(time.RFC3339, *args.StartTime)
		if err != nil {
			return nil, fmt.Errorf("query_chat_history start_time %q: %w", *args.StartTime, err)
		}
		ms := start.UnixMilli()
		startMS = &ms
	}
	if args.EndTime != nil && strings.TrimSpace(*args.EndTime) != "" {
		end, err := time.Parse(time.RFC3339, *args.EndTime)
		if err != nil {
			return nil, fmt.Errorf("query_chat_history end_time %q: %w", *args.EndTime, err)
		}
		ms := end.UnixMilli()
		endMS = &ms
	}

	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	query := t.db.WithContext(callCtx).Model(&domain.Message{}).Where("chat_id = ?", args.ChatID)
	if startMS != nil {
		query = query.Where("create_time >= ?", *startMS)
	}
	if endMS != nil {
		query = query.Where("create_time <= ?", *endMS)
	}
	if args.Keyword != nil && strings.TrimSpace(*args.Keyword) != "" {
		query = query.Where("content LIKE ?", "%"+likeEscape(strings.TrimSpace(*args.Keyword))+"%")
	}

	var rows []domain.Message
	if err := query.Order("create_time ASC, id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query_chat_history load messages chat_id=%q: %w", args.ChatID, err)
	}

	result := queryChatHistoryResult{ChatID: args.ChatID, Count: len(rows), Messages: make([]historyMessage, len(rows))}
	for i := range rows {
		result.Messages[i] = historyMessage{
			MessageID:  rows[i].MessageID,
			SenderName: rows[i].SenderName,
			Time:       time.UnixMilli(rows[i].CreateTime).In(t.location).Format(time.RFC3339),
			Content:    capRunes(rows[i].Content, historyContentRuneCap),
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("query_chat_history encode result: %w", err)
	}
	return encoded, nil
}

// likeEscape escapes the LIKE wildcards so a keyword containing % or _ matches
// literally rather than as a pattern.
func likeEscape(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}

func capRunes(value string, cap int) string {
	runes := []rune(value)
	if len(runes) <= cap {
		return value
	}
	return string(runes[:cap]) + "…"
}
