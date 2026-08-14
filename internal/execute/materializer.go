package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"
	"time"

	"gorm.io/gorm"
)

type MaterializationResult struct {
	TodoID      uint64 `json:"todo_id"`
	TodoVersion int32  `json:"todo_version"`
	TaskID      uint64 `json:"task_id"`
	TaskVersion int32  `json:"task_version"`
}

type MaterializationStats struct {
	Loaded       int
	Materialized int
	Failed       int
}

type Materializer struct {
	db *gorm.DB
}

func NewMaterializer(db *gorm.DB) (*Materializer, error) {
	if db == nil {
		return nil, fmt.Errorf("Todo materializer db is nil")
	}
	return &Materializer{db: db}, nil
}

func (m *Materializer) MaterializeOnce(ctx context.Context) (MaterializationStats, error) {
	var todos []domain.Todo
	if err := m.db.WithContext(ctx).
		Where("status = ?", "extracted").
		Order("is_leader_assigned DESC, last_evidence_at ASC, id ASC").
		Limit(50).
		Find(&todos).Error; err != nil {
		return MaterializationStats{}, fmt.Errorf("load extracted Todos: %w", err)
	}
	stats := MaterializationStats{Loaded: len(todos)}
	// One Todo that cannot be materialized must not hide the rest of the batch:
	// every failure is reported, but the remaining Todos still get their Task.
	var errs []error
	for i := range todos {
		if _, err := m.MaterializeTodo(ctx, todos[i].ID, todos[i].Version); err != nil {
			stats.Failed++
			errs = append(errs, fmt.Errorf("materialize todo_id=%d: %w", todos[i].ID, err))
			continue
		}
		stats.Materialized++
	}
	return stats, errors.Join(errs...)
}

func (m *Materializer) MaterializeTodo(ctx context.Context, todoID uint64, expectedVersion int32) (*MaterializationResult, error) {
	if todoID == 0 || expectedVersion < 0 {
		return nil, fmt.Errorf("%w: materialization Todo ID/version is invalid", ErrInvalidInput)
	}
	var result MaterializationResult
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var todo domain.Todo
		if err := lockTodo(tx, todoID, &todo); err != nil {
			return err
		}
		if todo.Version != expectedVersion {
			if todo.Status == "materialized" && todo.Version == expectedVersion+1 {
				task, err := loadTaskByTodo(tx, todo.ID)
				if err != nil {
					return err
				}
				result = materializationResult(todo.ID, todo.Version, task)
				return nil
			}
			return versionConflict(todo.ID, expectedVersion, todo.Version)
		}
		if todo.Status != "extracted" {
			return transitionError(todo.ID, todo.Status, "materialized")
		}
		background, err := todoExtractionResultAsTaskBackground(&todo)
		if err != nil {
			return err
		}
		existingTask, findErr := findTaskByTodo(tx, todo.ID)
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		update := tx.Model(&domain.Todo{}).
			Where("id = ? AND version = ? AND status = ?", todo.ID, expectedVersion, "extracted").
			Updates(map[string]any{"status": "materialized", "version": gorm.Expr("version + 1")})
		if update.Error != nil {
			return fmt.Errorf("materialize Todo id=%d: %w", todo.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return versionConflict(todo.ID, expectedVersion, todo.Version)
		}
		if existingTask != nil {
			if existingTask.Status != "observing" {
				return fmt.Errorf("%w: todo_id=%d task_id=%d status=%s", ErrTaskExists, todo.ID, existingTask.ID, existingTask.Status)
			}
			if err := createTodoEvent(tx, todo.ID, "extracted", "materialized", "materializer", map[string]any{
				"event_type": "task_rematerialized", "task_id": existingTask.ID,
				"reason": "fresh evidence reopened an observing Todo",
			}); err != nil {
				return err
			}
			rerunTask, err := resetTaskForRerun(tx, existingTask, "system", map[string]any{
				"reason": "fresh_todo_evidence", "todo_id": todo.ID, "todo_revision": todo.Revision,
			}, time.Now())
			if err != nil {
				return err
			}
			result = materializationResult(todo.ID, todo.Version+1, rerunTask)
			return nil
		}
		if err := createTodoEvent(tx, todo.ID, "extracted", "materialized", "materializer", map[string]any{
			"event_type": "task_materialized",
		}); err != nil {
			return err
		}
		factory, err := taskcreate.NewFactory(tx)
		if err != nil {
			return err
		}
		task, err := factory.CreateWithDB(ctx, tx, taskcreate.Input{
			TodoID: &todo.ID, Title: todo.Title, ActionType: todo.ActionType, Target: todo.Target,
			Background: background, SourcePayload: json.RawMessage(todo.ExtractionResult),
			ProjectID:  copyUint64(todo.ProjectID),
			SourceType: taskcreate.SourceTodo, SourceID: &todo.ID,
			ActorType: "system",
		})
		if errors.Is(err, taskcreate.ErrExists) {
			return fmt.Errorf("%w: todo_id=%d", ErrTaskExists, todo.ID)
		}
		if err != nil {
			return err
		}
		result = materializationResult(todo.ID, todo.Version+1, task)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func todoExtractionResultAsTaskBackground(todo *domain.Todo) (json.RawMessage, error) {
	raw := []byte(todo.ExtractionResult)
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: todo_id=%d extraction_result missing", ErrInvalidInput, todo.ID)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("%w: todo_id=%d extraction_result invalid: %v", ErrInvalidInput, todo.ID, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%w: todo_id=%d extraction_result must be object", ErrInvalidInput, todo.ID)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: todo_id=%d extraction_result has trailing bytes", ErrInvalidInput, todo.ID)
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("%w: todo_id=%d extraction_result encode: %v", ErrInvalidInput, todo.ID, err)
	}
	return json.RawMessage(encoded), nil
}

func lockTodo(tx *gorm.DB, todoID uint64, todo *domain.Todo) error {
	err := tx.First(todo, todoID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: todo_id=%d", ErrTodoNotFound, todoID)
	}
	if err != nil {
		return fmt.Errorf("lock Todo id=%d: %w", todoID, err)
	}
	return nil
}

func findTaskByTodo(db *gorm.DB, todoID uint64) (*domain.Task, error) {
	var task domain.Task
	if err := db.Where("todo_id = ?", todoID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func loadTaskByTodo(db *gorm.DB, todoID uint64) (*domain.Task, error) {
	task, err := findTaskByTodo(db, todoID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("materialized Todo id=%d has no Task", todoID)
	}
	if err != nil {
		return nil, fmt.Errorf("load Task for Todo id=%d: %w", todoID, err)
	}
	return task, nil
}

func materializationResult(todoID uint64, todoVersion int32, task *domain.Task) MaterializationResult {
	return MaterializationResult{
		TodoID: todoID, TodoVersion: todoVersion, TaskID: task.ID, TaskVersion: task.Version,
	}
}

func versionConflict(todoID uint64, expected, actual int32) error {
	return fmt.Errorf("%w: todo_id=%d expected=%d actual=%d", ErrVersionConflict, todoID, expected, actual)
}

func transitionError(todoID uint64, from, to string) error {
	return fmt.Errorf("%w: todo_id=%d from=%s to=%s", ErrInvalidTransition, todoID, from, to)
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
