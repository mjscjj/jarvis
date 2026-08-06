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
	"jarvis/internal/datatypes"
)

var (
	ErrTodoNotFound      = errors.New("todo not found")
	ErrInvalidTodoFilter = errors.New("invalid todo filter")
)

var allowedTodoStatuses = map[string]struct{}{
	"extracted": {}, "materialized": {}, "observing": {},
}

// m3OwnedTodoStatuses are the states re-extraction may still move a clue
// between. They are exactly the two values M3 can emit: a clue that has not yet
// been materialized, and one M3 judged as not needing anybody. Every other
// status was set downstream, so M3 leaves it alone.
var m3OwnedTodoStatuses = map[string]bool{"extracted": true, "observing": true}

type TodoListFilter struct {
	Statuses   []string
	ActionType string
	ProjectID  *uint64
	LeaderOnly *bool
	// From / Until narrow by last_evidence_at as a half-open RFC3339 window.
	// Callers own the timezone (same contract as FactFilter).
	From     *time.Time
	Until    *time.Time
	Page     int
	PageSize int
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
	Target             string           `json:"target"`
	Context            string           `json:"context"`
	OpenQuestions      json.RawMessage  `json:"open_questions"`
	CommitmentStrength string           `json:"commitment_strength"`
	SourceMessageIDs   json.RawMessage  `json:"source_message_ids"`
	SourceQuote        string           `json:"source_quote"`
	AssignerOpenID     *string          `json:"assigner_open_id"`
	IsLeaderAssigned   bool             `json:"is_leader_assigned"`
	DueAt              *time.Time       `json:"due_at"`
	Status             string           `json:"status"`
	Revision           int32            `json:"revision"`
	Version            int32            `json:"version"`
	FirstSeenAt        time.Time        `json:"first_seen_at"`
	LastEvidenceAt     time.Time        `json:"last_evidence_at"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	Group              *TodoGroupView   `json:"group"`
	Project            *TodoProjectView `json:"project"`
	// Resolution / ContextSnapshot are the M3-frozen project inference trace and
	// background, so the UI can show "why this project/repo" and M5 can query the
	// full creation-time context on demand (docs/design-context-pipeline.md §5/§6).
	Resolution      json.RawMessage `json:"resolution"`
	ContextSnapshot json.RawMessage `json:"context_snapshot"`
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

type TodoStatusWriter interface {
	SetTodoStatus(context.Context, TodoStatusInput) (*TodoView, error)
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
	if filter.From != nil {
		query = query.Where("last_evidence_at >= ?", filter.From.UTC())
	}
	if filter.Until != nil {
		query = query.Where("last_evidence_at < ?", filter.Until.UTC())
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count todos: %w", err)
	}
	var todos []domain.Todo
	offset := (filter.Page - 1) * filter.PageSize
	if err := query.Preload("Group").Preload("Project").
		Order("last_evidence_at DESC, id DESC").
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

// TodoStatusInput moves one clue between the two states that mean "nobody is
// acting on this right now". The principal uses it from the Todo list, and M5
// uses it through jarvis-tools when execution reveals there is nothing to do
// after all.
type TodoStatusInput struct {
	TodoID uint64
	Status string
	Actor  string
	Reason string
}

// observableTodoStatuses are the states this entry point may set. Everything
// else is owned by the stage that produces it — materialization writes materialized,
// execution and the principal write the rest — so re-pointing a clue by hand
// is limited to parking it (observing) or handing it back for materialization
// (extracted).
var observableTodoStatuses = map[string]bool{"observing": true, "extracted": true}

// SetTodoStatus parks a clue as observing or hands it back to materialization.
func (s *TodoStore) SetTodoStatus(ctx context.Context, input TodoStatusInput) (*TodoView, error) {
	if input.TodoID == 0 {
		return nil, fmt.Errorf("todo id must be positive")
	}
	if !observableTodoStatuses[input.Status] {
		return nil, fmt.Errorf("todo status %q cannot be set here, want observing or extracted", input.Status)
	}
	if strings.TrimSpace(input.Actor) == "" {
		return nil, fmt.Errorf("todo status change requires an actor")
	}
	if strings.TrimSpace(input.Reason) == "" {
		return nil, fmt.Errorf("todo status change requires a reason")
	}
	var todo domain.Todo
	err := s.db.WithContext(ctx).First(&todo, input.TodoID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: id=%d", ErrTodoNotFound, input.TodoID)
	}
	if err != nil {
		return nil, fmt.Errorf("load todo id=%d: %w", input.TodoID, err)
	}
	if err := validateTodoStatusTransition(todo.ID, todo.Status, input.Status); err != nil {
		return nil, err
	}
	from := todo.Status
	if from != input.Status {
		update := s.db.WithContext(ctx).Model(&domain.Todo{}).
			Where("id = ? AND version = ?", todo.ID, todo.Version).
			Updates(map[string]any{"status": input.Status, "version": gorm.Expr("version + 1")})
		if update.Error != nil {
			return nil, fmt.Errorf("set todo id=%d status: %w", todo.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return nil, fmt.Errorf("set todo id=%d status: concurrent update, retry", todo.ID)
		}
		if err := s.db.WithContext(ctx).First(&todo, input.TodoID).Error; err != nil {
			return nil, fmt.Errorf("reload todo id=%d: %w", input.TodoID, err)
		}
	}
	detail, err := json.Marshal(map[string]any{"event_type": "status_set", "reason": input.Reason})
	if err != nil {
		return nil, fmt.Errorf("encode todo status event detail: %w", err)
	}
	snapshot, err := domain.EncodeTodoEventSnapshot(&todo)
	if err != nil {
		return nil, err
	}
	event := domain.TodoEvent{
		TodoID: todo.ID, FromStatus: &from, ToStatus: input.Status,
		Actor: input.Actor, Detail: datatypes.JSON(detail), Snapshot: snapshot,
	}
	if err := s.db.WithContext(ctx).Create(&event).Error; err != nil {
		return nil, fmt.Errorf("create todo status event todo_id=%d: %w", todo.ID, err)
	}
	view := todoView(&todo)
	return &view, nil
}

func validateTodoStatusTransition(todoID uint64, from, to string) error {
	if _, live := activeTodoStatuses[from]; !live {
		return fmt.Errorf("todo id=%d is %s and cannot be re-opened", todoID, from)
	}
	if from == "materialized" && to == "extracted" {
		return fmt.Errorf("todo id=%d is materialized and already has a Task; rerun that Task instead", todoID)
	}
	return nil
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
		if !IsValidActionType(filter.ActionType) {
			return fmt.Errorf("%w: invalid action_type %q", ErrInvalidTodoFilter, filter.ActionType)
		}
	}
	return nil
}

func todoView(todo *domain.Todo) TodoView {
	view := TodoView{
		ID: todo.ID, Title: todo.Title, Description: todo.Description,
		ActionType: todo.ActionType, Target: todo.Target, Context: todo.Context,
		OpenQuestions:      rawJSON(todo.OpenQuestions),
		CommitmentStrength: todo.CommitmentStrength,
		SourceMessageIDs:   rawJSON(todo.SourceMessageIDs), SourceQuote: todo.SourceQuote,
		AssignerOpenID: todo.AssignerOpenID, IsLeaderAssigned: todo.IsLeaderAssigned,
		DueAt: todo.DueAt, Status: todo.Status,
		Revision: todo.Revision, Version: todo.Version, FirstSeenAt: todo.FirstSeenAt,
		LastEvidenceAt: todo.LastEvidenceAt, CreatedAt: todo.CreatedAt, UpdatedAt: todo.UpdatedAt,
		Resolution: rawJSON(todo.Resolution), ContextSnapshot: rawJSON(todo.ContextSnapshot),
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
