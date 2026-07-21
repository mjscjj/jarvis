package decide

import (
	"context"
	"errors"
	"fmt"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

type WorkerOptions struct {
	BatchLimit int
}

type WorkerStats struct {
	Loaded       int
	Evaluated    int
	Auto         int
	NeedInfo     int
	NeedDecision int
	Dropped      int
}

type evaluationSource interface {
	LoadExtracted(context.Context, int) ([]domain.Todo, error)
	LoadExtractedTodo(context.Context, uint64, int32) (*domain.Todo, error)
}

type todoEvaluator interface {
	Evaluate(context.Context, *domain.Todo) (*EvaluationInput, error)
}

type evaluationWriter interface {
	Apply(context.Context, EvaluationInput) (*EvaluationResult, error)
}

type EvaluationSource struct {
	db *gorm.DB
}

func NewEvaluationSource(db *gorm.DB) (*EvaluationSource, error) {
	if db == nil {
		return nil, fmt.Errorf("evaluation source db is nil")
	}
	return &EvaluationSource{db: db}, nil
}

func (s *EvaluationSource) LoadExtracted(ctx context.Context, limit int) ([]domain.Todo, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("evaluation source limit must be positive")
	}
	var todos []domain.Todo
	if err := s.db.WithContext(ctx).
		Where("status = ?", "extracted").
		Order("is_leader_assigned DESC, last_evidence_at ASC, id ASC").
		Limit(limit).
		Find(&todos).Error; err != nil {
		return nil, fmt.Errorf("load extracted Todos: %w", err)
	}
	return todos, nil
}

func (s *EvaluationSource) LoadExtractedTodo(ctx context.Context, todoID uint64, expectedVersion int32) (*domain.Todo, error) {
	if todoID == 0 || expectedVersion < 0 {
		return nil, fmt.Errorf("%w: evaluation Todo ID/version is invalid", ErrInvalidInput)
	}
	var todo domain.Todo
	if err := s.db.WithContext(ctx).First(&todo, todoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: todo_id=%d", ErrTodoNotFound, todoID)
		}
		return nil, fmt.Errorf("load extracted Todo id=%d: %w", todoID, err)
	}
	if todo.Version != expectedVersion {
		return nil, versionConflict(todo.ID, expectedVersion, todo.Version)
	}
	if todo.Status != "extracted" {
		return nil, transitionError(todo.ID, todo.Status, "evaluate")
	}
	return &todo, nil
}

type DecisionWorker struct {
	source    evaluationSource
	evaluator todoEvaluator
	writer    evaluationWriter
	opts      WorkerOptions
}

func NewDecisionWorker(source evaluationSource, evaluator todoEvaluator, writer evaluationWriter, opts WorkerOptions) (*DecisionWorker, error) {
	if source == nil {
		return nil, fmt.Errorf("decision worker source is nil")
	}
	if evaluator == nil {
		return nil, fmt.Errorf("decision worker evaluator is nil")
	}
	if writer == nil {
		return nil, fmt.Errorf("decision worker writer is nil")
	}
	if opts.BatchLimit <= 0 {
		return nil, fmt.Errorf("decision worker batch limit must be positive")
	}
	return &DecisionWorker{source: source, evaluator: evaluator, writer: writer, opts: opts}, nil
}

func (w *DecisionWorker) EvaluateOnce(ctx context.Context) (WorkerStats, error) {
	todos, err := w.source.LoadExtracted(ctx, w.opts.BatchLimit)
	if err != nil {
		return WorkerStats{}, err
	}
	stats := WorkerStats{Loaded: len(todos)}
	seen := make(map[uint64]struct{}, len(todos))
	for index := range todos {
		todo := &todos[index]
		if todo.ID == 0 {
			return stats, fmt.Errorf("evaluation source returned Todo with zero ID at position=%d", index)
		}
		if _, exists := seen[todo.ID]; exists {
			return stats, fmt.Errorf("evaluation source returned duplicate Todo id=%d", todo.ID)
		}
		seen[todo.ID] = struct{}{}
		if todo.Status != "extracted" {
			return stats, fmt.Errorf("evaluation source returned Todo id=%d status=%s", todo.ID, todo.Status)
		}
		result, err := w.evaluateLoaded(ctx, todo)
		if err != nil {
			return stats, err
		}
		stats.Evaluated++
		switch result.Status {
		case RouteAuto:
			stats.Auto++
		case RouteNeedInfo:
			stats.NeedInfo++
		case RouteNeedDecision:
			stats.NeedDecision++
		case RouteDropped:
			stats.Dropped++
		default:
			return stats, fmt.Errorf("persist Todo evaluation id=%d returned unsupported status=%s", todo.ID, result.Status)
		}
	}
	return stats, nil
}

// EvaluateTodo is the real-time M4 entry point. The caller passes the exact Todo
// version M3 committed; stale or duplicate notifications fail before any model
// call, while the scheduled batch path continues to reuse evaluateLoaded.
func (w *DecisionWorker) EvaluateTodo(ctx context.Context, todoID uint64, expectedVersion int32) (*EvaluationResult, error) {
	todo, err := w.source.LoadExtractedTodo(ctx, todoID, expectedVersion)
	if err != nil {
		return nil, err
	}
	return w.evaluateLoaded(ctx, todo)
}

func (w *DecisionWorker) evaluateLoaded(ctx context.Context, todo *domain.Todo) (*EvaluationResult, error) {
	input, err := w.evaluator.Evaluate(ctx, todo)
	if err != nil {
		return nil, fmt.Errorf("evaluate Todo id=%d: %w", todo.ID, err)
	}
	if input == nil {
		return nil, fmt.Errorf("evaluate Todo id=%d: nil result", todo.ID)
	}
	if input.TodoID != todo.ID || input.ExpectedVersion != todo.Version {
		return nil, fmt.Errorf(
			"evaluate Todo identity drift: loaded_id=%d loaded_version=%d result_id=%d result_version=%d",
			todo.ID, todo.Version, input.TodoID, input.ExpectedVersion,
		)
	}
	result, err := w.writer.Apply(ctx, *input)
	if err != nil {
		return nil, fmt.Errorf("persist Todo evaluation id=%d: %w", todo.ID, err)
	}
	if result == nil || result.TodoID != todo.ID || result.Status != input.Route || result.Version != todo.Version+1 {
		return nil, fmt.Errorf("persist Todo evaluation id=%d returned inconsistent result: %#v", todo.ID, result)
	}
	return result, nil
}
