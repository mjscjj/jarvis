package execute

import (
	"context"
	"fmt"
	"time"

	"jarvis/internal/domain"
)

// currentWorld is the slice of Jarvis' own work as it stands at the moment a run
// starts. It is the counterpart to Task.background: background froze the world
// when the clue was admitted, this is loaded fresh on every run.
//
// It exists to stop M5 from redoing work. The largest source of duplication is a
// Task that just finished, and no frozen snapshot can ever show that — by the
// time a Task executes, the snapshot may be hours or days old.
type currentWorld struct {
	LoadedAt    string      `json:"loaded_at"` // RFC3339 UTC
	RecentTasks []taskBrief `json:"recent_tasks"`
	OpenTodos   []todoBrief `json:"open_todos"`
}

type taskBrief struct {
	ID             uint64  `json:"id"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	Summary        string  `json:"summary,omitempty"`
	LastProgressAt string  `json:"last_progress_at,omitempty"`
	ProjectID      *uint64 `json:"project_id,omitempty"`
}

type todoBrief struct {
	ID             uint64  `json:"id"`
	Title          string  `json:"title"`
	Target         string  `json:"target,omitempty"`
	Status         string  `json:"status"`
	LastEvidenceAt string  `json:"last_evidence_at,omitempty"`
	GroupID        *uint64 `json:"group_id,omitempty"`
	ProjectID      *uint64 `json:"project_id,omitempty"`
}

const (
	currentWorldTaskLimit = 20
	currentWorldTodoLimit = 20
)

// loadCurrentWorld reads the newest Tasks and open Todos for the execution
// prompt. Both lists are summaries only; M5 drills into any row with get-task /
// get-todo and widens the range with list-tasks / list-todos.
func (e *AgentExecutor) loadCurrentWorld(ctx context.Context, excludeTaskID uint64) (*currentWorld, error) {
	tasks, err := e.loadRecentTaskBriefs(ctx, excludeTaskID)
	if err != nil {
		return nil, err
	}
	todos, err := e.loadOpenTodoBriefs(ctx)
	if err != nil {
		return nil, err
	}
	return &currentWorld{
		LoadedAt:    e.now().UTC().Format(time.RFC3339),
		RecentTasks: tasks,
		OpenTodos:   todos,
	}, nil
}

// loadRecentTaskBriefs deliberately does not filter by status: `done` and
// `failed` are the rows that answer "has someone already handled this", which is
// the whole point of this block. Ordering by last progress keeps the window on
// what actually moved recently rather than on what is merely still open.
func (e *AgentExecutor) loadRecentTaskBriefs(ctx context.Context, excludeTaskID uint64) ([]taskBrief, error) {
	type row struct {
		ID             uint64
		Title          string
		Status         string
		Summary        *string
		LastProgressAt *time.Time
		ProjectID      *uint64
	}
	query := e.store.db.WithContext(ctx).Model(&domain.Task{})
	if excludeTaskID != 0 {
		query = query.Where("id <> ?", excludeTaskID)
	}
	var rows []row
	if err := query.
		Select("id, title, status, summary, last_progress_at, project_id").
		Order("COALESCE(last_progress_at, created_at) DESC, id DESC").
		Limit(currentWorldTaskLimit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load current world Tasks: %w", err)
	}
	result := make([]taskBrief, len(rows))
	for i := range rows {
		result[i] = taskBrief{
			ID: rows[i].ID, Title: rows[i].Title, Status: rows[i].Status,
			ProjectID: rows[i].ProjectID,
		}
		if rows[i].Summary != nil {
			result[i].Summary = *rows[i].Summary
		}
		if rows[i].LastProgressAt != nil {
			result[i].LastProgressAt = rows[i].LastProgressAt.UTC().Format(time.RFC3339)
		}
	}
	return result, nil
}

// loadOpenTodoBriefs returns clues that are still open. `extracted` rarely has
// any backlog because materialization drains it within seconds, so in practice
// this is the recent `observing` stream: things worth knowing that nobody has
// been asked to act on.
func (e *AgentExecutor) loadOpenTodoBriefs(ctx context.Context) ([]todoBrief, error) {
	var rows []domain.Todo
	if err := e.store.db.WithContext(ctx).
		Where("status IN ?", []string{"extracted", "observing"}).
		Order("last_evidence_at DESC, id DESC").
		Limit(currentWorldTodoLimit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load current world Todos: %w", err)
	}
	result := make([]todoBrief, len(rows))
	for i := range rows {
		result[i] = todoBrief{
			ID: rows[i].ID, Title: rows[i].Title, Target: rows[i].Target,
			Status:         rows[i].Status,
			LastEvidenceAt: rows[i].LastEvidenceAt.UTC().Format(time.RFC3339),
			GroupID:        rows[i].GroupID, ProjectID: rows[i].ProjectID,
		}
	}
	return result, nil
}
