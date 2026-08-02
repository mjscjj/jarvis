// Package insight aggregates cross-module read-only views for the admin UI:
// the Overview dashboard (current counts) and the daily Progress digest.
// It owns no tables; every number is a live aggregation over todo/task/message.
package insight

import (
	"context"
	"fmt"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// OverviewService serves the Overview dashboard tab. All numbers are computed
// on read; there is no cache and no cron.
type OverviewService struct {
	db *gorm.DB
}

func NewOverviewService(db *gorm.DB) (*OverviewService, error) {
	if db == nil {
		return nil, fmt.Errorf("overview service db is nil")
	}
	return &OverviewService{db: db}, nil
}

// StatusCount is one (status, count) pair.
type StatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

// Overview is the whole dashboard payload: todo/task status breakdowns plus a
// few headline totals the UI turns into summary cards.
type Overview struct {
	Todos struct {
		Total      int64         `json:"total"`
		Open       int64         `json:"open"`        // extracted（等待机械物化）
		LeaderOpen int64         `json:"leader_open"` // leader 交办且未闭环
		ByStatus   []StatusCount `json:"by_status"`
	} `json:"todos"`
	Tasks struct {
		Total   int64 `json:"total"`
		Pending int64 `json:"pending"` // pending/executing/waiting/needs_human
		// NeedsMe counts the only human gate left in the pipeline: M5 parked a Task
		// because it wants the principal to approve a proposal or answer a question.
		NeedsMe  int64         `json:"needs_me"`
		Done     int64         `json:"done"`
		Failed   int64         `json:"failed"`
		ByStatus []StatusCount `json:"by_status"`
	} `json:"tasks"`
}

// openTodoStatuses are Todo states still waiting for mechanical Task
// materialization. A Todo never waits on the user: M3 either leaves it observing
// (nobody has to act) or marks it extracted, after which the materializer creates
// a Task and changes it to materialized. Anything that needs the user is raised
// by M5 on that Task. Observing belongs to none of these counts because counting
// it as open would put clues nobody is working on back in the overview backlog.
var openTodoStatuses = []string{"extracted"}

func (s *OverviewService) Load(ctx context.Context) (*Overview, error) {
	overview := &Overview{}

	todoCounts, err := s.groupCount(ctx, &domain.Todo{})
	if err != nil {
		return nil, fmt.Errorf("count todos by status: %w", err)
	}
	overview.Todos.ByStatus = todoCounts
	for _, item := range todoCounts {
		overview.Todos.Total += item.Count
		if contains(openTodoStatuses, item.Status) {
			overview.Todos.Open += item.Count
		}
	}
	if err := s.db.WithContext(ctx).Model(&domain.Todo{}).
		Where("is_leader_assigned = ? AND status IN ?", true, openTodoStatuses).
		Count(&overview.Todos.LeaderOpen).Error; err != nil {
		return nil, fmt.Errorf("count leader open todos: %w", err)
	}

	taskCounts, err := s.groupCount(ctx, &domain.Task{})
	if err != nil {
		return nil, fmt.Errorf("count tasks by status: %w", err)
	}
	overview.Tasks.ByStatus = taskCounts
	for _, item := range taskCounts {
		overview.Tasks.Total += item.Count
		switch item.Status {
		case "pending", "executing", "waiting":
			overview.Tasks.Pending += item.Count
		case "needs_human", "awaiting_approval":
			overview.Tasks.Pending += item.Count
			overview.Tasks.NeedsMe += item.Count
		case "done":
			overview.Tasks.Done += item.Count
		case "failed":
			overview.Tasks.Failed += item.Count
		}
	}
	return overview, nil
}

// groupCount runs SELECT status, COUNT(*) GROUP BY status for a table.
func (s *OverviewService) groupCount(ctx context.Context, model any) ([]StatusCount, error) {
	rows := []StatusCount{}
	if err := s.db.WithContext(ctx).Model(model).
		Select("status, COUNT(*) AS count").
		Group("status").
		Order("count DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
