// Package execute owns the MVP Task execution lifecycle.
package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTaskNotFound      = errors.New("execution Task not found")
	ErrVersionConflict   = errors.New("execution version conflict")
	ErrInvalidTransition = errors.New("invalid execution transition")
	ErrInvalidInput      = errors.New("invalid execution input")
)

var taskStatuses = map[string]struct{}{
	"pending": {}, "executing": {}, "done": {}, "failed": {},
}

type TaskFilter struct {
	Statuses []string
	Page     int
	PageSize int
}

type TaskList struct {
	Items    []TaskView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type TaskView struct {
	ID              uint64          `json:"id"`
	TodoID          uint64          `json:"todo_id"`
	Title           string          `json:"title"`
	ActionType      string          `json:"action_type"`
	Background      json.RawMessage `json:"background"`
	Plan            json.RawMessage `json:"plan"`
	Slots           json.RawMessage `json:"slots"`
	ConfirmedBy     string          `json:"confirmed_by"`
	ConfirmedAt     time.Time       `json:"confirmed_at"`
	ActionHash      string          `json:"action_hash"`
	Status          string          `json:"status"`
	ExecutionResult json.RawMessage `json:"execution_result"`
	AutonomyMode    string          `json:"autonomy_mode"`
	ProjectID       *uint64         `json:"project_id"`
	Version         int32           `json:"version"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type FinishInput struct {
	TaskID          uint64
	ExpectedVersion int32
	Status          string
	Result          json.RawMessage
}

type TaskService interface {
	ListTasks(context.Context, TaskFilter) (*TaskList, error)
	Finish(context.Context, FinishInput) (*TaskView, error)
}

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("execution store db is nil")
	}
	return &Store{db: db}, nil
}

func (s *Store) ListTasks(ctx context.Context, filter TaskFilter) (*TaskList, error) {
	if err := ValidateTaskFilter(filter); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&domain.Task{})
	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count execution Tasks: %w", err)
	}
	var rows []domain.Task
	if err := query.Order("confirmed_at ASC, id ASC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list execution Tasks: %w", err)
	}
	items := make([]TaskView, len(rows))
	for i := range rows {
		items[i] = taskView(&rows[i])
	}
	return &TaskList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *Store) Finish(ctx context.Context, input FinishInput) (*TaskView, error) {
	if input.TaskID == 0 || input.ExpectedVersion < 0 {
		return nil, fmt.Errorf("%w: Task ID/version is invalid", ErrInvalidInput)
	}
	if input.Status != "done" && input.Status != "failed" {
		return nil, fmt.Errorf("%w: status must be done or failed", ErrInvalidInput)
	}
	result, err := canonicalJSONObject(input.Result)
	if err != nil {
		return nil, err
	}
	var finished domain.Task
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task domain.Task
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, input.TaskID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, input.TaskID)
		}
		if err != nil {
			return fmt.Errorf("lock execution Task id=%d: %w", input.TaskID, err)
		}
		if task.Version != input.ExpectedVersion {
			return fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, input.ExpectedVersion, task.Version)
		}
		if task.Status != "pending" && task.Status != "executing" {
			return fmt.Errorf("%w: task_id=%d from=%s to=%s", ErrInvalidTransition, task.ID, task.Status, input.Status)
		}
		fromStatus := task.Status
		update := tx.Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, input.ExpectedVersion, fromStatus).
			Updates(map[string]any{
				"status": input.Status, "execution_result": datatypes.JSON(result), "version": gorm.Expr("version + 1"),
			})
		if update.Error != nil {
			return fmt.Errorf("finish execution Task id=%d: %w", task.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, input.ExpectedVersion)
		}
		task.Status = input.Status
		task.ExecutionResult = datatypes.JSON(result)
		task.Version++
		finished = task
		return nil
	})
	if err != nil {
		return nil, err
	}
	view := taskView(&finished)
	return &view, nil
}

// MarkExecuting transitions a Task from pending to executing under optimistic
// lock and returns the new version. It is the guard that prevents two runners
// from grabbing the same Task concurrently (manual button + cron).
func (s *Store) MarkExecuting(ctx context.Context, taskID uint64, expectedVersion int32) (int32, error) {
	if taskID == 0 || expectedVersion < 0 {
		return 0, fmt.Errorf("%w: Task ID/version is invalid", ErrInvalidInput)
	}
	var newVersion int32
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task domain.Task
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, taskID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		if err != nil {
			return fmt.Errorf("lock execution Task id=%d: %w", taskID, err)
		}
		if task.Version != expectedVersion {
			return fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, expectedVersion, task.Version)
		}
		if task.Status != "pending" {
			return fmt.Errorf("%w: task_id=%d from=%s to=executing", ErrInvalidTransition, task.ID, task.Status)
		}
		update := tx.Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, expectedVersion, "pending").
			Updates(map[string]any{"status": "executing", "version": gorm.Expr("version + 1")})
		if update.Error != nil {
			return fmt.Errorf("mark executing Task id=%d: %w", task.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, expectedVersion)
		}
		newVersion = task.Version + 1
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newVersion, nil
}

// LoadPending returns pending Tasks for the cron auto-executor, oldest first.
func (s *Store) LoadPending(ctx context.Context, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: pending load limit must be positive", ErrInvalidInput)
	}
	var rows []domain.Task
	if err := s.db.WithContext(ctx).
		Where("status = ?", "pending").
		Order("confirmed_at ASC, id ASC").
		Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load pending execution Tasks: %w", err)
	}
	return rows, nil
}

func ValidateTaskFilter(filter TaskFilter) error {
	if filter.Page <= 0 || filter.PageSize <= 0 || filter.PageSize > 100 {
		return fmt.Errorf("%w: page must be positive and page_size must be between 1 and 100", ErrInvalidInput)
	}
	for _, status := range filter.Statuses {
		if _, ok := taskStatuses[status]; !ok {
			return fmt.Errorf("%w: unsupported Task status %q", ErrInvalidInput, status)
		}
	}
	return nil
}

func ParseStatuses(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	var statuses []string
	for _, part := range strings.Split(value, ",") {
		status := strings.TrimSpace(part)
		if status == "" {
			return nil, fmt.Errorf("%w: status contains an empty value", ErrInvalidInput)
		}
		if _, ok := taskStatuses[status]; !ok {
			return nil, fmt.Errorf("%w: unsupported Task status %q", ErrInvalidInput, status)
		}
		if _, ok := seen[status]; !ok {
			seen[status] = struct{}{}
			statuses = append(statuses, status)
		}
	}
	return statuses, nil
}

func canonicalJSONObject(raw []byte) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: result is required", ErrInvalidInput)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("%w: decode result: %v", ErrInvalidInput, err)
	}
	if len(object) == 0 {
		return nil, fmt.Errorf("%w: result must be a non-empty JSON object", ErrInvalidInput)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: result must contain one JSON object", ErrInvalidInput)
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("encode execution result: %w", err)
	}
	return encoded, nil
}

func taskView(task *domain.Task) TaskView {
	return TaskView{
		ID: task.ID, TodoID: task.TodoID, Title: task.Title, ActionType: task.ActionType,
		Background: rawJSON(task.Background), Plan: rawJSON(task.Plan), Slots: rawJSON(task.Slots),
		ConfirmedBy: task.ConfirmedBy, ConfirmedAt: task.ConfirmedAt, ActionHash: task.ActionHash,
		Status: task.Status, ExecutionResult: rawJSON(task.ExecutionResult), AutonomyMode: task.AutonomyMode,
		ProjectID: task.ProjectID, Version: task.Version, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), value...))
}
