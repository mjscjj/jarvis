package decide

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/memory"

	"gorm.io/gorm"
)

type memorySearcher interface {
	Search(context.Context, memory.SearchInput) (*memory.SearchResponse, error)
}

type BackgroundOptions struct {
	MemoryTopK      int
	MemoryThreshold float64
}

type backgroundSnapshotter interface {
	Snapshot(context.Context, *domain.Todo) (json.RawMessage, error)
}

type BackgroundSnapshotter struct {
	db     *gorm.DB
	memory memorySearcher
	opts   BackgroundOptions
	now    func() time.Time
}

func NewBackgroundSnapshotter(db *gorm.DB, memories memorySearcher, opts BackgroundOptions) (*BackgroundSnapshotter, error) {
	if db == nil {
		return nil, fmt.Errorf("background snapshotter db is nil")
	}
	if memories == nil {
		return nil, fmt.Errorf("background snapshotter memory client is nil")
	}
	if opts.MemoryTopK <= 0 {
		return nil, fmt.Errorf("background snapshotter memory top_k must be positive")
	}
	if opts.MemoryThreshold < 0 || opts.MemoryThreshold > 1 {
		return nil, fmt.Errorf("background snapshotter memory threshold must be between 0 and 1")
	}
	return &BackgroundSnapshotter{db: db, memory: memories, opts: opts, now: time.Now}, nil
}

func (s *BackgroundSnapshotter) Snapshot(ctx context.Context, todo *domain.Todo) (json.RawMessage, error) {
	if todo == nil || todo.ID == 0 {
		return nil, fmt.Errorf("snapshot background Todo is invalid")
	}
	var sourceIDs []string
	if err := json.Unmarshal(todo.SourceMessageIDs, &sourceIDs); err != nil {
		return nil, fmt.Errorf("decode Todo source message IDs todo_id=%d: %w", todo.ID, err)
	}
	if len(sourceIDs) == 0 {
		return nil, fmt.Errorf("Todo source message IDs are empty todo_id=%d", todo.ID)
	}
	var rows []domain.Message
	if err := s.db.WithContext(ctx).Where("message_id IN ?", sourceIDs).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load Todo source messages todo_id=%d: %w", todo.ID, err)
	}
	byID := make(map[string]domain.Message, len(rows))
	for _, row := range rows {
		byID[row.MessageID] = row
	}
	messages := make([]backgroundMessage, 0, len(sourceIDs))
	for _, messageID := range sourceIDs {
		row, ok := byID[messageID]
		if !ok {
			return nil, fmt.Errorf("Todo source message %q not found todo_id=%d", messageID, todo.ID)
		}
		messages = append(messages, backgroundMessage{
			MessageID: row.MessageID, ChatID: row.ChatID, SenderOpenID: row.SenderOpenID,
			SenderName: row.SenderName, Content: row.Content, CreateTime: row.CreateTime,
		})
	}

	var project *backgroundProject
	if todo.ProjectID != nil {
		var row domain.Project
		if err := s.db.WithContext(ctx).First(&row, *todo.ProjectID).Error; err != nil {
			return nil, fmt.Errorf("load Todo project todo_id=%d project_id=%d: %w", todo.ID, *todo.ProjectID, err)
		}
		project = &backgroundProject{
			ID: row.ID, Code: row.Code, Name: row.Name, Role: row.Role, Description: row.Description,
			Repos: rawJSON(row.Repos), KeyDecisions: rawJSON(row.KeyDecisions),
		}
	}

	var group *backgroundGroup
	filters := make(map[string]any)
	if todo.GroupID != nil {
		var row domain.Group
		if err := s.db.WithContext(ctx).First(&row, *todo.GroupID).Error; err != nil {
			return nil, fmt.Errorf("load Todo group todo_id=%d group_id=%d: %w", todo.ID, *todo.GroupID, err)
		}
		group = &backgroundGroup{ID: row.ID, ChatID: row.ChatID, Name: row.Name}
		filters["chat_id"] = row.ChatID
	}
	if todo.ProjectID != nil {
		filters = map[string]any{"project_id": *todo.ProjectID}
	}

	var assigner *backgroundAssigner
	if todo.AssignerOpenID != nil {
		assigner = &backgroundAssigner{OpenID: *todo.AssignerOpenID}
		var row domain.Person
		result := s.db.WithContext(ctx).Where("open_id = ?", *todo.AssignerOpenID).Limit(1).Find(&row)
		if result.Error != nil {
			return nil, fmt.Errorf("load Todo assigner todo_id=%d: %w", todo.ID, result.Error)
		}
		if result.RowsAffected == 1 {
			assigner.Name = &row.Name
			assigner.Role = &row.Role
			assigner.Title = row.Title
			assigner.Relation = row.Relation
		}
	}

	query := strings.TrimSpace(todo.Title + "\n" + todo.Description)
	memories, err := s.memory.Search(ctx, memory.SearchInput{
		Query: query, Filters: filters, TopK: s.opts.MemoryTopK,
		Threshold: s.opts.MemoryThreshold, Rerank: false,
	})
	if err != nil {
		return nil, fmt.Errorf("search Task background memories todo_id=%d: %w", todo.ID, err)
	}
	if memories == nil {
		return nil, fmt.Errorf("search Task background memories todo_id=%d: nil response", todo.ID)
	}

	snapshot := backgroundSnapshot{
		CapturedAt: s.now().UTC(), TodoID: todo.ID, TodoRevision: todo.Revision,
		Project: project, Group: group, Assigner: assigner, Messages: messages,
		Memories: memories.Results,
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode Task background todo_id=%d: %w", todo.ID, err)
	}
	return json.RawMessage(encoded), nil
}

type backgroundSnapshot struct {
	CapturedAt   time.Time           `json:"captured_at"`
	TodoID       uint64              `json:"todo_id"`
	TodoRevision int32               `json:"todo_revision"`
	Project      *backgroundProject  `json:"project"`
	Group        *backgroundGroup    `json:"group"`
	Assigner     *backgroundAssigner `json:"assigner"`
	Messages     []backgroundMessage `json:"messages"`
	Memories     []map[string]any    `json:"memories"`
}

type backgroundProject struct {
	ID           uint64          `json:"id"`
	Code         *string         `json:"code"`
	Name         string          `json:"name"`
	Role         string          `json:"role"`
	Description  *string         `json:"description"`
	Repos        json.RawMessage `json:"repos"`
	KeyDecisions json.RawMessage `json:"key_decisions"`
}

type backgroundGroup struct {
	ID     uint64  `json:"id"`
	ChatID string  `json:"chat_id"`
	Name   *string `json:"name"`
}

type backgroundAssigner struct {
	OpenID   string  `json:"open_id"`
	Name     *string `json:"name"`
	Role     *string `json:"role"`
	Title    *string `json:"title"`
	Relation *string `json:"relation"`
}

type backgroundMessage struct {
	MessageID    string `json:"message_id"`
	ChatID       string `json:"chat_id"`
	SenderOpenID string `json:"sender_open_id"`
	SenderName   string `json:"sender_name"`
	Content      string `json:"content"`
	CreateTime   int64  `json:"create_time"`
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), value...))
}
