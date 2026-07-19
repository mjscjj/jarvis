package extract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var (
	ErrTodoNotFound      = errors.New("todo not found")
	ErrInvalidTodoFilter = errors.New("invalid todo filter")
)

var allowedTodoStatuses = map[string]struct{}{
	"extracted": {}, "scoring": {}, "need_info": {}, "need_decision": {},
	"confirmed": {}, "dismissed": {}, "expired": {},
}

type TodoListFilter struct {
	Statuses   []string
	ActionType string
	ProjectID  *uint64
	LeaderOnly *bool
	Page       int
	PageSize   int
}

type TodoList struct {
	Items    []TodoView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type TodoView struct {
	ID                 uint64           `json:"id"`
	Title              string           `json:"title"`
	Description        string           `json:"description"`
	ActionType         string           `json:"action_type"`
	Slots              json.RawMessage  `json:"slots"`
	CommitmentStrength string           `json:"commitment_strength"`
	SourceMessageIDs   json.RawMessage  `json:"source_message_ids"`
	SourceQuote        string           `json:"source_quote"`
	AssignerOpenID     *string          `json:"assigner_open_id"`
	IsLeaderAssigned   bool             `json:"is_leader_assigned"`
	DueAt              *time.Time       `json:"due_at"`
	Status             string           `json:"status"`
	Confidence         *float64         `json:"confidence"`
	Risk               *float64         `json:"risk"`
	Route              *string          `json:"route"`
	MissingInfo        json.RawMessage  `json:"missing_info"`
	Revision           int32            `json:"revision"`
	Version            int32            `json:"version"`
	FirstSeenAt        time.Time        `json:"first_seen_at"`
	LastEvidenceAt     time.Time        `json:"last_evidence_at"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	Group              *TodoGroupView   `json:"group"`
	Project            *TodoProjectView `json:"project"`
}

type TodoGroupView struct {
	ID     uint64  `json:"id"`
	ChatID string  `json:"chat_id"`
	Name   *string `json:"name"`
}

type TodoProjectView struct {
	ID   uint64  `json:"id"`
	Code *string `json:"code"`
	Name string  `json:"name"`
}

type TodoReader interface {
	ListTodos(context.Context, TodoListFilter) (*TodoList, error)
	GetTodo(context.Context, uint64) (*TodoView, error)
}

type TodoStore struct {
	db *gorm.DB
}

func NewTodoStore(db *gorm.DB) (*TodoStore, error) {
	if db == nil {
		return nil, fmt.Errorf("todo store db is nil")
	}
	return &TodoStore{db: db}, nil
}

func (s *TodoStore) ListTodos(ctx context.Context, filter TodoListFilter) (*TodoList, error) {
	if err := ValidateTodoFilter(filter); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&domain.Todo{})
	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}
	if filter.ActionType != "" {
		query = query.Where("action_type = ?", filter.ActionType)
	}
	if filter.ProjectID != nil {
		query = query.Where("project_id = ?", *filter.ProjectID)
	}
	if filter.LeaderOnly != nil {
		query = query.Where("is_leader_assigned = ?", *filter.LeaderOnly)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count todos: %w", err)
	}
	var todos []domain.Todo
	offset := (filter.Page - 1) * filter.PageSize
	if err := query.Preload("Group").Preload("Project").
		Order("is_leader_assigned DESC, last_evidence_at DESC, id DESC").
		Offset(offset).Limit(filter.PageSize).Find(&todos).Error; err != nil {
		return nil, fmt.Errorf("list todos: %w", err)
	}
	items := make([]TodoView, len(todos))
	for i := range todos {
		items[i] = todoView(&todos[i])
	}
	return &TodoList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *TodoStore) GetTodo(ctx context.Context, id uint64) (*TodoView, error) {
	if id == 0 {
		return nil, fmt.Errorf("todo id must be positive")
	}
	var todo domain.Todo
	err := s.db.WithContext(ctx).Preload("Group").Preload("Project").First(&todo, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: id=%d", ErrTodoNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get todo id=%d: %w", id, err)
	}
	view := todoView(&todo)
	return &view, nil
}

func ValidateTodoFilter(filter TodoListFilter) error {
	if filter.Page <= 0 {
		return fmt.Errorf("%w: page must be positive", ErrInvalidTodoFilter)
	}
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		return fmt.Errorf("%w: page_size must be between 1 and 100", ErrInvalidTodoFilter)
	}
	for _, status := range filter.Statuses {
		if _, ok := allowedTodoStatuses[status]; !ok {
			return fmt.Errorf("%w: unsupported status %q", ErrInvalidTodoFilter, status)
		}
	}
	if filter.ActionType != "" {
		if _, ok := requiredSlots[filter.ActionType]; !ok {
			return fmt.Errorf("%w: unsupported action_type %q", ErrInvalidTodoFilter, filter.ActionType)
		}
	}
	return nil
}

func todoView(todo *domain.Todo) TodoView {
	view := TodoView{
		ID: todo.ID, Title: todo.Title, Description: todo.Description,
		ActionType: todo.ActionType, Slots: rawJSON(todo.Slots),
		CommitmentStrength: todo.CommitmentStrength,
		SourceMessageIDs:   rawJSON(todo.SourceMessageIDs), SourceQuote: todo.SourceQuote,
		AssignerOpenID: todo.AssignerOpenID, IsLeaderAssigned: todo.IsLeaderAssigned,
		DueAt: todo.DueAt, Status: todo.Status, Confidence: todo.Confidence,
		Risk: todo.Risk, Route: todo.Route, MissingInfo: rawJSON(todo.MissingInfo),
		Revision: todo.Revision, Version: todo.Version, FirstSeenAt: todo.FirstSeenAt,
		LastEvidenceAt: todo.LastEvidenceAt, CreatedAt: todo.CreatedAt, UpdatedAt: todo.UpdatedAt,
	}
	if todo.Group != nil {
		view.Group = &TodoGroupView{ID: todo.Group.ID, ChatID: todo.Group.ChatID, Name: todo.Group.Name}
	}
	if todo.Project != nil {
		view.Project = &TodoProjectView{ID: todo.Project.ID, Code: todo.Project.Code, Name: todo.Project.Name}
	}
	return view
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), value...))
}

func ParseStatuses(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("%w: status contains an empty value", ErrInvalidTodoFilter)
		}
		if _, ok := seen[part]; !ok {
			seen[part] = struct{}{}
			result = append(result, part)
		}
	}
	return result, nil
}
