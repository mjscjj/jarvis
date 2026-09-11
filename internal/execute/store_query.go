package execute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/contextpack"
	"jarvis/internal/domain"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/gorm"
)

// LoadTask returns the persistence entity used by the execution orchestrator.
// Keeping this read in Store prevents orchestration code from depending on
// GORM or duplicating not-found translation at every transition.
func (s *Store) LoadTask(ctx context.Context, taskID uint64) (*domain.Task, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: Task ID is invalid", ErrInvalidInput)
	}
	var task domain.Task
	if err := s.db.WithContext(ctx).First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return nil, fmt.Errorf("load execution Task id=%d: %w", taskID, err)
	}
	return &task, nil
}

func (s *Store) LoadRun(ctx context.Context, runID uint64) (*domain.ExecutionRun, error) {
	if runID == 0 {
		return nil, fmt.Errorf("%w: execution run ID is invalid", ErrInvalidInput)
	}
	var run domain.ExecutionRun
	if err := s.db.WithContext(ctx).First(&run, runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: run_id=%d", ErrRunNotFound, runID)
		}
		return nil, fmt.Errorf("load execution run id=%d: %w", runID, err)
	}
	return &run, nil
}

// SaveRun inserts a run on its first call and updates it in place afterwards.
// A run is written twice: once as running before the agent is invoked, so a
// crash mid-invocation leaves evidence that work may have started, and once
// with its terminal state.
func (s *Store) SaveRun(ctx context.Context, run *domain.ExecutionRun) error {
	if run == nil || run.TaskID == 0 {
		return fmt.Errorf("%w: execution run is invalid", ErrInvalidInput)
	}
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Save(run).Error; err != nil {
		return fmt.Errorf("save execution run task_id=%d: %w", run.TaskID, err)
	}
	return nil
}

func (s *Store) ListTasks(ctx context.Context, filter TaskFilter) (*TaskList, error) {
	if err := ValidateTaskFilter(filter); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&domain.Task{})
	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}
	if filter.ActionType != "" {
		query = query.Where("action_type = ?", filter.ActionType)
	}
	if filter.ExcludeActionType != "" {
		query = query.Where("action_type <> ?", filter.ExcludeActionType)
	}
	if filter.ProjectID != nil {
		query = query.Where("project_id = ?", *filter.ProjectID)
	}
	if filter.GroupID != nil {
		// Subquery rather than JOIN: Count and Find below both run off this same
		// query, and a JOIN would make their column sets diverge.
		query = query.Where("todo_id IN (?)",
			s.db.WithContext(ctx).Model(&domain.Todo{}).Select("id").Where("group_id = ?", *filter.GroupID))
	}
	if filter.From != nil {
		query = query.Where("COALESCE(last_progress_at, created_at) >= ?", filter.From.UTC())
	}
	if filter.Until != nil {
		query = query.Where("COALESCE(last_progress_at, created_at) < ?", filter.Until.UTC())
	}
	if filter.Query != "" {
		term := "%" + filter.Query + "%"
		query = query.Where("(title LIKE ? OR target LIKE ? OR summary LIKE ? OR json_extract(source_payload, '$.source') LIKE ? OR json_extract(source_payload, '$.annotation.brief') LIKE ? OR json_extract(source_payload, '$.capture.messages') LIKE ?)", term, term, term, term, term, term)
	}
	if filter.SourceMessageID != "" {
		query = query.Where("EXISTS (SELECT 1 FROM json_each(task.source_payload, '$.source.source_message_ids') WHERE value = ?)", filter.SourceMessageID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count execution Tasks: %w", err)
	}
	var rows []domain.Task
	if err := query.Order("updated_at DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list execution Tasks: %w", err)
	}
	items := make([]TaskView, len(rows))
	for i := range rows {
		items[i] = taskView(ctx, &rows[i])
		items[i].SourceMessageIDs = contextpack.SourceMessageIDs(rows[i].SourcePayload)
		if err := projectTaskListItem(&items[i]); err != nil {
			return nil, fmt.Errorf("project Task id=%d: %w", rows[i].ID, err)
		}
	}
	if err := s.attachTaskResolutions(ctx, items); err != nil {
		return nil, err
	}
	return &TaskList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

// GetTask returns one Task by id.
func (s *Store) GetTask(ctx context.Context, taskID uint64) (*TaskView, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: Task ID is invalid", ErrInvalidInput)
	}
	var row domain.Task
	err := s.db.WithContext(ctx).First(&row, taskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("get Task id=%d: %w", taskID, err)
	}
	view := taskView(ctx, &row)
	if resolution, err := s.taskResolution(ctx, row.ID); err != nil {
		return nil, err
	} else {
		view.Resolution = resolution
	}
	return &view, nil
}

// ListRuns returns a Task's execution audit history, newest first. It is the
// read path over execution_run (previously write-only) that powers the task
// detail drawer. An unknown task_id simply yields an empty list.
type RunFilter struct{ Page, PageSize int }

func (s *Store) GetRun(ctx context.Context, id uint64) (*RunView, error) {
	row, err := s.LoadRun(ctx, id)
	if err != nil {
		return nil, err
	}
	view := runView(row)
	return &view, nil
}
func (s *Store) ListRuns(ctx context.Context, taskID uint64, f RunFilter) (*RunList, error) {
	if taskID == 0 || f.Page < 1 || f.PageSize < 1 || f.PageSize > 100 {
		return nil, fmt.Errorf("%w: invalid Task ID or run pagination", ErrInvalidInput)
	}
	q := s.db.WithContext(ctx).Model(&domain.ExecutionRun{}).Where("task_id = ?", taskID)
	result := &RunList{}
	if err := q.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	result.Page = f.Page
	result.PageSize = f.PageSize
	q = q.Omit("prompt", "output", "effects", "error_detail").Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize)
	var rows []domain.ExecutionRun
	if err := q.Order("started_at DESC,id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result.Items = make([]RunView, len(rows))
	for i := range rows {
		result.Items[i] = runView(&rows[i])
	}
	return result, nil
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

func (s *Store) taskResolution(ctx context.Context, taskID uint64) (*TaskResolutionView, error) {
	views := []TaskView{{ID: taskID}}
	if err := s.attachTaskResolutions(ctx, views); err != nil {
		return nil, err
	}
	return views[0].Resolution, nil
}

func (s *Store) attachTaskResolutions(ctx context.Context, tasks []TaskView) error {
	ids := make([]uint64, 0, len(tasks))
	byID := make(map[uint64]*TaskView, len(tasks))
	for i := range tasks {
		if !isTerminalTaskStatus(tasks[i].Status) && tasks[i].Status != "" {
			continue
		}
		ids = append(ids, tasks[i].ID)
		byID[tasks[i].ID] = &tasks[i]
	}
	if len(ids) == 0 {
		return nil
	}
	var events []domain.TaskEvent
	if err := s.db.WithContext(ctx).
		Where("task_id IN ? AND to_status IN ?", ids, []string{"done", "failed", "observing"}).
		Order("task_id ASC, id DESC").Find(&events).Error; err != nil {
		return fmt.Errorf("load Task resolution events: %w", err)
	}
	seen := make(map[uint64]struct{}, len(ids))
	for i := range events {
		event := &events[i]
		if _, ok := seen[event.TaskID]; ok {
			continue
		}
		task := byID[event.TaskID]
		if task == nil {
			continue
		}
		task.Resolution = &TaskResolutionView{
			EventType: event.EventType, ActorType: event.ActorType,
			ActorRef: event.ActorRef, OccurredAt: event.OccurredAt,
		}
		seen[event.TaskID] = struct{}{}
	}
	return nil
}

func taskView(ctx context.Context, task *domain.Task) TaskView {
	supplements, err := decodeExecutionSupplements(task.ExecutionSupplements)
	if err != nil {
		// 写入侧 Supplement 已严格校验，正常不会存进坏数据；一旦解析失败说明库里
		// 的 execution_supplements 被损坏。这里 taskView 无法返回 error，至少打点
		// 暴露问题（不静默吞掉，符合 fail-fast），补充信息在本次视图中缺省为空。
		ctx = observability.EnsureLogID(ctx)
		hlog.CtxErrorf(ctx, "decode execution supplements failed task_id=%d error=%+v", task.ID, err)
		supplements = nil
	}
	return TaskView{
		ID: task.ID, TodoID: task.TodoID, Title: task.Title, ActionType: task.ActionType,
		Target:        task.Target,
		SourcePayload: rawJSON(task.SourcePayload),
		SourceURL:     contextpack.SourceURL(task.SourcePayload),
		SourceType:    task.SourceType, SourceID: task.SourceID, OccurrenceKey: task.OccurrenceKey,
		Status: task.Status, ExecutionResult: rawJSON(task.ExecutionResult),
		Summary: task.Summary, LastProgressAt: task.LastProgressAt,
		ExecutionSupplements: supplements,
		ProjectID:            task.ProjectID, RepoPath: task.RepoPath,
		Version: task.Version, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

func runView(run *domain.ExecutionRun) RunView {
	return RunView{
		ID: run.ID, TaskID: run.TaskID, ActionType: run.ActionType, Stage: run.Stage, Sandbox: run.Sandbox,
		Status: run.Status, Prompt: run.Prompt, CodexSessionID: run.CodexSessionID, Summary: run.Summary,
		Output: rawJSON(run.Output), Effects: rawJSON(run.Effects), ErrorDetail: run.ErrorDetail,
		RepoPath:  run.RepoPath,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, DurationMs: run.DurationMs,
	}
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), value...))
}
