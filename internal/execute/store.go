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
	"jarvis/internal/observability"
	"jarvis/internal/progress"

	"code.byted.org/middleware/hertz/pkg/common/hlog"
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
	"pending": {}, "executing": {}, "waiting": {}, "needs_human": {}, "awaiting_approval": {}, "done": {}, "failed": {}, "observing": {},
}

type TaskFilter struct {
	Statuses []string
	// From / Until narrow by COALESCE(last_progress_at, confirmed_at) as a
	// half-open RFC3339 window. Callers own the timezone.
	From     *time.Time
	Until    *time.Time
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
	ID                   uint64                `json:"id"`
	TodoID               *uint64               `json:"todo_id"`
	Title                string                `json:"title"`
	ActionType           string                `json:"action_type"`
	Target               string                `json:"target"`
	Background           json.RawMessage       `json:"background"`
	Plan                 json.RawMessage       `json:"plan"`
	ConfirmedBy          string                `json:"confirmed_by"`
	ConfirmedAt          time.Time             `json:"confirmed_at"`
	SourceType           string                `json:"source_type"`
	SourceID             *uint64               `json:"source_id"`
	OccurrenceKey        *string               `json:"occurrence_key"`
	ExecutionMode        string                `json:"execution_mode"`
	Status               string                `json:"status"`
	ExecutionResult      json.RawMessage       `json:"execution_result"`
	Summary              *string               `json:"summary"`
	LastProgressAt       *time.Time            `json:"last_progress_at"`
	ExecutionSupplements []ExecutionSupplement `json:"execution_supplements,omitempty"`
	ProjectID            *uint64               `json:"project_id"`
	Version              int32                 `json:"version"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

type FinishInput struct {
	TaskID          uint64
	ExpectedVersion int32
	Status          string
	Result          json.RawMessage
	ActorType       string
	ActorRef        *string
	RunID           *uint64
	EventType       string
}

type SupplementInput struct {
	TaskID          uint64
	ExpectedVersion int32
	Note            string
	Channel         string
}

type HumanResumeClaim struct {
	TaskID      uint64
	SourceRunID uint64
	Version     int32
	Response    string
}

// RunView 是一次 ExecutionRun 审计记录的只读视图，供任务详情展示执行历史。
// Prompt 原样返回，便于在任务详情中核对模型收到的完整输入。
type RunView struct {
	ID             uint64          `json:"id"`
	TaskID         uint64          `json:"task_id"`
	ActionType     string          `json:"action_type"`
	Stage          string          `json:"stage"`
	Sandbox        string          `json:"sandbox"`
	Status         string          `json:"status"`
	Prompt         string          `json:"prompt"`
	CodexSessionID *string         `json:"codex_session_id"`
	Summary        *string         `json:"summary"`
	Output         json.RawMessage `json:"output"`
	Effects        json.RawMessage `json:"effects"`
	ErrorDetail    *string         `json:"error_detail"`
	RepoPath       *string         `json:"repo_path"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at"`
	DurationMs     *int64          `json:"duration_ms"`
}

type RunList struct {
	Items []RunView `json:"items"`
}

type TaskService interface {
	ListTasks(context.Context, TaskFilter) (*TaskList, error)
	GetTask(context.Context, uint64) (*TaskView, error)
	Finish(context.Context, FinishInput) (*TaskView, error)
	Supplement(context.Context, SupplementInput) (*TaskView, error)
	ListRuns(context.Context, uint64) (*RunList, error)
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
	if filter.From != nil {
		query = query.Where("COALESCE(last_progress_at, confirmed_at) >= ?", filter.From.UTC())
	}
	if filter.Until != nil {
		query = query.Where("COALESCE(last_progress_at, confirmed_at) < ?", filter.Until.UTC())
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
	return &view, nil
}

func (s *Store) Finish(ctx context.Context, input FinishInput) (*TaskView, error) {
	if input.TaskID == 0 || input.ExpectedVersion < 0 {
		return nil, fmt.Errorf("%w: Task ID/version is invalid", ErrInvalidInput)
	}
	if input.Status != "done" && input.Status != "failed" && input.Status != "observing" {
		return nil, fmt.Errorf("%w: status must be done, failed or observing", ErrInvalidInput)
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
		if err := closeUnboundContinuations(tx, task.ID, "agent finished without a matching waiting outcome"); err != nil {
			return err
		}
		if input.Status == "observing" {
			if err := parkClueAsObserving(tx, &task); err != nil {
				return err
			}
		}
		task.Status = input.Status
		task.ExecutionResult = datatypes.JSON(result)
		task.Version++
		eventType := "execution_succeeded"
		switch input.Status {
		case "failed":
			eventType = "execution_failed"
		case "observing":
			eventType = "execution_observing"
		}
		if input.EventType != "" {
			eventType = input.EventType
		}
		if err := progress.AppendTaskEvent(tx, progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: task.Version, EventType: eventType,
			FromStatus: &fromStatus, ToStatus: input.Status,
			ActorType: input.ActorType, ActorRef: input.ActorRef, RunID: input.RunID,
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		finished = task
		return nil
	})
	if err != nil {
		return nil, err
	}
	view := taskView(ctx, &finished)
	return &view, nil
}

// RecordProgress stores where the matter now stands, as M5 described it at the
// end of a run.
//
// It sits outside the status-transition methods and deliberately does not bump
// version: version is the optimistic-lock token those transitions race on, so
// bumping it for a summary would make progress writes collide with a concurrent
// claim. Progress is an observation about the work, not a state change to it.
//
// last_progress_at moves only when the summary actually changes. A Task that
// keeps resuming and re-reporting the same standing is not making progress, and
// treating it as such would hide exactly the stalled work this field exists to
// surface. A blank summary is therefore a no-op, not an erasure.
func (s *Store) RecordProgress(ctx context.Context, taskID uint64, summary string, now time.Time) error {
	if taskID == 0 {
		return fmt.Errorf("%w: Task ID is invalid", ErrInvalidInput)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}
	var task domain.Task
	if err := s.db.WithContext(ctx).Select("id", "summary").First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return fmt.Errorf("load execution Task id=%d for progress: %w", taskID, err)
	}
	if task.Summary != nil && strings.TrimSpace(*task.Summary) == summary {
		return nil
	}
	at := now.UTC()
	if err := s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ?", taskID).
		Updates(map[string]any{"summary": summary, "last_progress_at": at}).Error; err != nil {
		return fmt.Errorf("record progress task_id=%d: %w", taskID, err)
	}
	return nil
}

var supplementableTaskStatuses = map[string]struct{}{
	"pending": {}, "executing": {}, "waiting": {}, "needs_human": {}, "awaiting_approval": {}, "done": {}, "failed": {}, "observing": {},
}

// Supplement appends a human clarification/instruction to a Task's M5-only
// execution_supplements. It does not touch Todo.context_snapshot or Task.plan.
func (s *Store) Supplement(ctx context.Context, input SupplementInput) (*TaskView, error) {
	if input.TaskID == 0 || input.ExpectedVersion < 0 {
		return nil, fmt.Errorf("%w: Task ID/version is invalid", ErrInvalidInput)
	}
	note := strings.TrimSpace(input.Note)
	if note == "" {
		return nil, fmt.Errorf("%w: supplement note must be non-blank", ErrInvalidInput)
	}
	channel := strings.TrimSpace(input.Channel)
	if channel == "" {
		channel = "backend"
	}

	var task domain.Task
	if err := s.db.WithContext(ctx).First(&task, input.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, input.TaskID)
		}
		return nil, fmt.Errorf("load execution Task id=%d: %w", input.TaskID, err)
	}
	if task.Version != input.ExpectedVersion {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, input.ExpectedVersion, task.Version)
	}
	if _, ok := supplementableTaskStatuses[task.Status]; !ok {
		return nil, fmt.Errorf("%w: task_id=%d status=%s cannot be supplemented", ErrInvalidTransition, task.ID, task.Status)
	}

	encoded, err := appendExecutionSupplement(task.ExecutionSupplements, note, channel, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("append execution_supplements task_id=%d: %w", task.ID, err)
	}

	result := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("id = ? AND version = ? AND status = ?", task.ID, input.ExpectedVersion, task.Status).
		Updates(map[string]any{
			"execution_supplements": datatypes.JSON(encoded),
			"version":               gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("apply supplement task_id=%d: %w", task.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, input.ExpectedVersion)
	}

	var reloaded domain.Task
	if err := s.db.WithContext(ctx).First(&reloaded, task.ID).Error; err != nil {
		return nil, fmt.Errorf("reload execution Task id=%d after supplement: %w", task.ID, err)
	}
	if err := progress.AppendTaskEvent(s.db.WithContext(ctx), progress.TaskEventInput{
		TaskID: reloaded.ID, TaskVersion: reloaded.Version, EventType: "supplemented",
		FromStatus: &reloaded.Status, ToStatus: reloaded.Status, ActorType: "user",
		Detail: map[string]any{"channel": channel}, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return nil, err
	}
	view := taskView(ctx, &reloaded)
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
		if err := closeUnboundContinuations(tx, task.ID, "agent requested approval without a matching waiting outcome"); err != nil {
			return err
		}
		newVersion = task.Version + 1
		fromStatus := "pending"
		if err := progress.AppendTaskEvent(tx, progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: newVersion, EventType: "execution_started",
			FromStatus: &fromStatus, ToStatus: "executing", ActorType: "m5",
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newVersion, nil
}

// MarkAwaitingApproval parks an executing Task at awaiting_approval after the
// propose stage decided the external write is high-risk. It stores the approved-
// pending proposal (the plan + full artifact codex produced without touching the
// outside world) into execution_result so the UI can render it and the later
// apply stage can replay it. It bumps the version and returns the new version.
func (s *Store) MarkAwaitingApproval(ctx context.Context, taskID uint64, expectedVersion int32, runID uint64, proposal json.RawMessage) (int32, error) {
	if taskID == 0 || expectedVersion < 0 || runID == 0 {
		return 0, fmt.Errorf("%w: Task ID/version is invalid", ErrInvalidInput)
	}
	result, err := canonicalJSONObject(proposal)
	if err != nil {
		return 0, err
	}
	var newVersion int32
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if task.Status != "executing" {
			return fmt.Errorf("%w: task_id=%d from=%s to=awaiting_approval", ErrInvalidTransition, task.ID, task.Status)
		}
		update := tx.Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, expectedVersion, "executing").
			Updates(map[string]any{
				"status": "awaiting_approval", "execution_result": datatypes.JSON(result), "version": gorm.Expr("version + 1"),
			})
		if update.Error != nil {
			return fmt.Errorf("mark awaiting approval Task id=%d: %w", task.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, expectedVersion)
		}
		newVersion = task.Version + 1
		fromStatus := "executing"
		if err := progress.AppendTaskEvent(tx, progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: newVersion, EventType: "approval_requested",
			FromStatus: &fromStatus, ToStatus: "awaiting_approval", ActorType: "m5",
			RunID: &runID, OccurredAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newVersion, nil
}

// MarkWaiting parks an executing Task after the agent successfully created a
// resume_task schedule. The schedule is bound to the exact run whose Codex
// session must be resumed.
func (s *Store) MarkWaiting(ctx context.Context, taskID uint64, expectedVersion int32, runID, scheduledTaskID uint64, result json.RawMessage) (int32, error) {
	if taskID == 0 || expectedVersion < 0 || runID == 0 || scheduledTaskID == 0 {
		return 0, fmt.Errorf("%w: waiting Task/run/schedule identity is invalid", ErrInvalidInput)
	}
	canonical, err := canonicalJSONObject(result)
	if err != nil {
		return 0, err
	}
	var run domain.ExecutionRun
	if err := s.db.WithContext(ctx).First(&run, runID).Error; err != nil {
		return 0, fmt.Errorf("load waiting execution run id=%d: %w", runID, err)
	}
	if run.TaskID != taskID || run.Status != "waiting" || run.CodexSessionID == nil || strings.TrimSpace(*run.CodexSessionID) == "" {
		return 0, fmt.Errorf("%w: run_id=%d is not a resumable waiting run for task_id=%d", ErrInvalidInput, runID, taskID)
	}
	bind := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND dispatch_kind = ? AND subject_type = ? AND subject_id = ? AND source_run_id IS NULL AND status = ?",
			scheduledTaskID, "resume_task", "task", taskID, "binding").
		Updates(map[string]any{"source_run_id": runID, "status": "active"})
	if bind.Error != nil {
		return 0, fmt.Errorf("bind scheduled task id=%d to run id=%d: %w", scheduledTaskID, runID, bind.Error)
	}
	if bind.RowsAffected != 1 {
		return 0, fmt.Errorf("%w: scheduled_task_id=%d is not an unbound resume for task_id=%d", ErrInvalidTransition, scheduledTaskID, taskID)
	}
	if err := closeOtherUnboundContinuations(s.db.WithContext(ctx), taskID, scheduledTaskID); err != nil {
		return 0, err
	}
	update := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("id = ? AND version = ? AND status = ?", taskID, expectedVersion, "executing").
		Updates(map[string]any{
			"status": "waiting", "execution_result": datatypes.JSON(canonical), "version": gorm.Expr("version + 1"),
		})
	if update.Error != nil {
		return 0, fmt.Errorf("mark waiting Task id=%d: %w", taskID, update.Error)
	}
	if update.RowsAffected != 1 {
		return 0, fmt.Errorf("%w: task_id=%d expected=%d from=executing to=waiting", ErrVersionConflict, taskID, expectedVersion)
	}
	newVersion := expectedVersion + 1
	fromStatus := "executing"
	if err := progress.AppendTaskEvent(s.db.WithContext(ctx), progress.TaskEventInput{
		TaskID: taskID, TaskVersion: newVersion, EventType: "waiting_scheduled",
		FromStatus: &fromStatus, ToStatus: "waiting", ActorType: "m5", RunID: &runID,
		Detail: map[string]any{"scheduled_task_id": scheduledTaskID}, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return 0, err
	}
	return newVersion, nil
}

// MarkNeedsHuman parks an executing Task without turning it into a failure.
// The exact run and Codex session are persisted so a later user response can
// resume the same execution conversation instead of starting the Task over.
func (s *Store) MarkNeedsHuman(ctx context.Context, taskID uint64, expectedVersion int32, runID uint64, result json.RawMessage) (int32, error) {
	if taskID == 0 || expectedVersion < 0 || runID == 0 {
		return 0, fmt.Errorf("%w: needs_human Task/run identity is invalid", ErrInvalidInput)
	}
	canonical, err := canonicalJSONObject(result)
	if err != nil {
		return 0, err
	}
	var run domain.ExecutionRun
	if err := s.db.WithContext(ctx).First(&run, runID).Error; err != nil {
		return 0, fmt.Errorf("load needs_human execution run id=%d: %w", runID, err)
	}
	if run.TaskID != taskID || run.Status != "needs_human" || run.CodexSessionID == nil || strings.TrimSpace(*run.CodexSessionID) == "" {
		return 0, fmt.Errorf("%w: run_id=%d is not a resumable needs_human run for task_id=%d", ErrInvalidInput, runID, taskID)
	}
	update := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("id = ? AND version = ? AND status = ?", taskID, expectedVersion, "executing").
		Updates(map[string]any{
			"status": "needs_human", "execution_result": datatypes.JSON(canonical), "version": gorm.Expr("version + 1"),
		})
	if update.Error != nil {
		return 0, fmt.Errorf("mark needs_human Task id=%d: %w", taskID, update.Error)
	}
	if update.RowsAffected != 1 {
		return 0, fmt.Errorf("%w: task_id=%d expected=%d from=executing to=needs_human", ErrVersionConflict, taskID, expectedVersion)
	}
	newVersion := expectedVersion + 1
	fromStatus := "executing"
	if err := progress.AppendTaskEvent(s.db.WithContext(ctx), progress.TaskEventInput{
		TaskID: taskID, TaskVersion: newVersion, EventType: "human_input_requested",
		FromStatus: &fromStatus, ToStatus: "needs_human", ActorType: "m5", RunID: &runID,
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		return 0, err
	}
	return newVersion, nil
}

// parkClueAsObserving moves the originating clue back to observing when the
// execution step concluded nobody needs to act.
//
// Execution decides after actually investigating, so it can find out the matter
// is real but asks nothing of anyone. Leaving the clue on "auto" would keep
// claiming a Task is driving it. observing is a live status for dedup, so
// re-seeing the same matter updates this clue instead of minting a second one,
// and fresh evidence can pull it back to extracted for another execution.
//
// A Task without a Todo (a scheduled run, say) has no clue to park.
func parkClueAsObserving(db *gorm.DB, task *domain.Task) error {
	if task.TodoID == nil {
		return nil
	}
	var todo domain.Todo
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&todo, *task.TodoID).Error; err != nil {
		return fmt.Errorf("lock clue id=%d for observing task_id=%d: %w", *task.TodoID, task.ID, err)
	}
	// A re-run of an already-parked Task lands here a second time.
	if todo.Status == "observing" {
		return nil
	}
	if todo.Status != "auto" {
		return fmt.Errorf("%w: todo_id=%d from=%s to=observing", ErrInvalidTransition, todo.ID, todo.Status)
	}
	update := db.Model(&domain.Todo{}).
		Where("id = ? AND status = ?", todo.ID, "auto").
		Updates(map[string]any{"status": "observing", "version": gorm.Expr("version + 1")})
	if update.Error != nil {
		return fmt.Errorf("park clue id=%d as observing: %w", todo.ID, update.Error)
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("%w: todo_id=%d from=auto to=observing", ErrVersionConflict, todo.ID)
	}
	return createTodoEvent(db, todo.ID, "auto", "observing", map[string]any{
		"event_type": "parked_observing",
		"reason":     "execution investigated and found nothing anyone needs to act on",
		"task_id":    task.ID,
	})
}

func closeUnboundContinuations(db *gorm.DB, taskID uint64, reason string) error {
	now := time.Now().UTC()
	result := db.Model(&domain.ScheduledTask{}).
		Where("dispatch_kind = ? AND subject_type = ? AND subject_id = ? AND source_run_id IS NULL AND status = ?",
			"resume_task", "task", taskID, "binding").
		Updates(map[string]any{
			"status": "completed", "last_run_status": "failed",
			"last_error_detail": reason, "last_finished_at": now,
		})
	if result.Error != nil {
		return fmt.Errorf("close unbound continuation schedules for task_id=%d: %w", taskID, result.Error)
	}
	return nil
}

func closeOtherUnboundContinuations(db *gorm.DB, taskID, selectedID uint64) error {
	now := time.Now().UTC()
	result := db.Model(&domain.ScheduledTask{}).
		Where("id <> ? AND dispatch_kind = ? AND subject_type = ? AND subject_id = ? AND source_run_id IS NULL AND status = ?",
			selectedID, "resume_task", "task", taskID, "binding").
		Updates(map[string]any{
			"status": "completed", "last_run_status": "failed",
			"last_error_detail": "superseded by the continuation selected in the agent result", "last_finished_at": now,
		})
	if result.Error != nil {
		return fmt.Errorf("close extra continuation schedules for task_id=%d: %w", taskID, result.Error)
	}
	return nil
}

// ClaimWaiting resumes one parked Task. The exact source run guards the
// transition so a stale or duplicate scheduled trigger cannot start it twice.
func (s *Store) ClaimWaiting(ctx context.Context, taskID, sourceRunID uint64) (int32, error) {
	if taskID == 0 || sourceRunID == 0 {
		return 0, fmt.Errorf("%w: waiting Task/source run identity is invalid", ErrInvalidInput)
	}
	var task domain.Task
	if err := s.db.WithContext(ctx).First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return 0, fmt.Errorf("load waiting Task id=%d: %w", taskID, err)
	}
	var run domain.ExecutionRun
	if err := s.db.WithContext(ctx).First(&run, sourceRunID).Error; err != nil {
		return 0, fmt.Errorf("load source run id=%d: %w", sourceRunID, err)
	}
	if run.TaskID != taskID || run.Status != "waiting" || run.CodexSessionID == nil || strings.TrimSpace(*run.CodexSessionID) == "" {
		return 0, fmt.Errorf("%w: source_run_id=%d is not resumable for task_id=%d", ErrInvalidInput, sourceRunID, taskID)
	}
	update := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("id = ? AND version = ? AND status = ?", task.ID, task.Version, "waiting").
		Updates(map[string]any{"status": "executing", "version": gorm.Expr("version + 1")})
	if update.Error != nil {
		return 0, fmt.Errorf("claim waiting Task id=%d: %w", taskID, update.Error)
	}
	if update.RowsAffected != 1 {
		return 0, fmt.Errorf("%w: task_id=%d from=%s to=executing", ErrInvalidTransition, taskID, task.Status)
	}
	newVersion := task.Version + 1
	fromStatus := "waiting"
	if err := progress.AppendTaskEvent(s.db.WithContext(ctx), progress.TaskEventInput{
		TaskID: taskID, TaskVersion: newVersion, EventType: "resumed",
		FromStatus: &fromStatus, ToStatus: "executing", ActorType: "scheduled_task", RunID: &sourceRunID,
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		return 0, err
	}
	return newVersion, nil
}

// ClaimNeedsHuman appends the user's response and claims a parked Task for
// continuation. It binds the continuation to the exact needs_human run so stale
// UI clicks cannot resume an older Codex session.
func (s *Store) ClaimNeedsHuman(ctx context.Context, taskID uint64, expectedVersion int32, response, channel string) (*HumanResumeClaim, error) {
	if taskID == 0 || expectedVersion < 0 {
		return nil, fmt.Errorf("%w: needs_human Task ID/version is invalid", ErrInvalidInput)
	}
	response = strings.TrimSpace(response)
	if response == "" {
		return nil, fmt.Errorf("%w: human response must be non-blank", ErrInvalidInput)
	}
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "backend"
	}
	var task domain.Task
	if err := s.db.WithContext(ctx).First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return nil, fmt.Errorf("load needs_human Task id=%d: %w", taskID, err)
	}
	if task.Version != expectedVersion {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, expectedVersion, task.Version)
	}
	if task.Status != "needs_human" {
		return nil, fmt.Errorf("%w: task_id=%d from=%s to=executing", ErrInvalidTransition, task.ID, task.Status)
	}
	sourceRunID, err := needsHumanSourceRunID(task.ExecutionResult)
	if err != nil {
		return nil, fmt.Errorf("read needs_human source run task_id=%d: %w", task.ID, err)
	}
	var run domain.ExecutionRun
	if err := s.db.WithContext(ctx).First(&run, sourceRunID).Error; err != nil {
		return nil, fmt.Errorf("load needs_human source run id=%d: %w", sourceRunID, err)
	}
	if run.TaskID != task.ID || run.Status != "needs_human" || run.CodexSessionID == nil || strings.TrimSpace(*run.CodexSessionID) == "" {
		return nil, fmt.Errorf("%w: source_run_id=%d is not resumable for task_id=%d", ErrInvalidInput, sourceRunID, task.ID)
	}
	encoded, err := appendExecutionSupplement(task.ExecutionSupplements, response, channel, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("append human response task_id=%d: %w", task.ID, err)
	}
	update := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("id = ? AND version = ? AND status = ?", task.ID, expectedVersion, "needs_human").
		Updates(map[string]any{
			"status": "executing", "execution_supplements": datatypes.JSON(encoded), "version": gorm.Expr("version + 1"),
		})
	if update.Error != nil {
		return nil, fmt.Errorf("claim needs_human Task id=%d: %w", task.ID, update.Error)
	}
	if update.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d from=needs_human to=executing", ErrVersionConflict, task.ID, expectedVersion)
	}
	newVersion := expectedVersion + 1
	fromStatus := "needs_human"
	if err := progress.AppendTaskEvent(s.db.WithContext(ctx), progress.TaskEventInput{
		TaskID: task.ID, TaskVersion: newVersion, EventType: "human_response_received",
		FromStatus: &fromStatus, ToStatus: "executing", ActorType: "user", RunID: &sourceRunID,
		Detail: map[string]any{"channel": channel}, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return nil, err
	}
	return &HumanResumeClaim{
		TaskID: task.ID, SourceRunID: sourceRunID, Version: newVersion, Response: response,
	}, nil
}

func needsHumanSourceRunID(raw []byte) (uint64, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return 0, fmt.Errorf("%w: needs_human execution_result is empty", ErrInvalidInput)
	}
	var stored struct {
		Outcome     string `json:"outcome"`
		SourceRunID uint64 `json:"source_run_id"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return 0, fmt.Errorf("%w: decode needs_human execution_result: %v", ErrInvalidInput, err)
	}
	if stored.Outcome != "needs_human" || stored.SourceRunID == 0 {
		return 0, fmt.Errorf("%w: execution_result is not a resumable needs_human result", ErrInvalidInput)
	}
	return stored.SourceRunID, nil
}

// MarkExecutingFromApproval claims an awaiting_approval Task for the apply stage
// (awaiting_approval -> executing) under optimistic lock and returns the new
// version. It is the concurrency guard for Approve, mirroring MarkExecuting for
// the propose stage.
func (s *Store) MarkExecutingFromApproval(ctx context.Context, taskID uint64, expectedVersion int32) (int32, error) {
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
		if task.Status != "awaiting_approval" {
			return fmt.Errorf("%w: task_id=%d from=%s to=executing", ErrInvalidTransition, task.ID, task.Status)
		}
		update := tx.Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, expectedVersion, "awaiting_approval").
			Updates(map[string]any{"status": "executing", "version": gorm.Expr("version + 1")})
		if update.Error != nil {
			return fmt.Errorf("mark executing (apply) Task id=%d: %w", task.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, expectedVersion)
		}
		newVersion = task.Version + 1
		fromStatus := "awaiting_approval"
		if err := progress.AppendTaskEvent(tx, progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: newVersion, EventType: "approval_granted",
			FromStatus: &fromStatus, ToStatus: "executing", ActorType: "user",
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newVersion, nil
}

// RejectAwaitingApproval transitions an awaiting_approval Task to failed when the
// human declines the proposed external write. It records the rejection reason in
// execution_result (overwriting the proposal) so the UI shows why, and the Task
// can later be rerun. It bumps the version and returns the reloaded Task.
func (s *Store) RejectAwaitingApproval(ctx context.Context, taskID uint64, expectedVersion int32, result json.RawMessage) (*TaskView, error) {
	if taskID == 0 || expectedVersion < 0 {
		return nil, fmt.Errorf("%w: Task ID/version is invalid", ErrInvalidInput)
	}
	canonical, err := canonicalJSONObject(result)
	if err != nil {
		return nil, err
	}
	var rejected domain.Task
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if task.Status != "awaiting_approval" {
			return fmt.Errorf("%w: task_id=%d from=%s to=failed (reject)", ErrInvalidTransition, task.ID, task.Status)
		}
		update := tx.Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, expectedVersion, "awaiting_approval").
			Updates(map[string]any{
				"status": "failed", "execution_result": datatypes.JSON(canonical), "version": gorm.Expr("version + 1"),
			})
		if update.Error != nil {
			return fmt.Errorf("reject execution Task id=%d: %w", task.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, expectedVersion)
		}
		newVersion := task.Version + 1
		fromStatus := "awaiting_approval"
		if err := progress.AppendTaskEvent(tx, progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: newVersion, EventType: "approval_rejected",
			FromStatus: &fromStatus, ToStatus: "failed", ActorType: "user",
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		if err := tx.First(&rejected, taskID).Error; err != nil {
			return fmt.Errorf("reload execution Task id=%d after reject: %w", task.ID, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	view := taskView(ctx, &rejected)
	return &view, nil
}

// ResetForRerun transitions a finished Task (done/failed) back to pending so it
// can be executed again, clearing the previous execution_result. It bumps the
// version (optimistic lock) and returns the reloaded Task. A task that is not
// finished (pending/executing) is rejected — you cannot "rerun" one that never
// finished or is mid-flight.
func (s *Store) ResetForRerun(ctx context.Context, taskID uint64) (*domain.Task, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: Task ID is invalid", ErrInvalidInput)
	}
	var reloaded domain.Task
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task domain.Task
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, taskID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		if err != nil {
			return fmt.Errorf("lock execution Task id=%d: %w", taskID, err)
		}
		// observing reruns like any other terminal state: "nobody needs to act"
		// was a verdict on the evidence at the time, and new evidence can overturn
		// it. The clue stays observing until the rerun concludes otherwise.
		if task.Status != "done" && task.Status != "failed" && task.Status != "observing" {
			return fmt.Errorf("%w: task_id=%d from=%s to=pending (only finished Tasks can rerun)", ErrInvalidTransition, task.ID, task.Status)
		}
		update := tx.Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, task.Version, task.Status).
			Updates(map[string]any{"status": "pending", "execution_result": nil, "version": gorm.Expr("version + 1")})
		if update.Error != nil {
			return fmt.Errorf("reset execution Task id=%d for rerun: %w", task.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: task_id=%d", ErrVersionConflict, task.ID)
		}
		newVersion := task.Version + 1
		fromStatus := task.Status
		if err := progress.AppendTaskEvent(tx, progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: newVersion, EventType: "rerun_requested",
			FromStatus: &fromStatus, ToStatus: "pending", ActorType: "user",
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		if err := tx.First(&reloaded, taskID).Error; err != nil {
			return fmt.Errorf("reload execution Task id=%d after reset: %w", task.ID, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &reloaded, nil
}

// ClaimForReapply claims a failed Task for a re-apply of its already-approved
// proposal (failed -> executing) under optimistic lock, returning the new
// version. Unlike rerun (which restarts propose and re-requests approval), this
// re-lands the SAME artifact a human already approved, so it only accepts a Task
// whose last landing attempt (apply stage) failed — the caller verifies an
// approved proposal is recoverable before invoking this. It does not clear the
// old execution_result until the new run finishes (finishRun overwrites it).
func (s *Store) ClaimForReapply(ctx context.Context, taskID uint64, expectedVersion int32) (int32, error) {
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
		if task.Status != "failed" {
			return fmt.Errorf("%w: task_id=%d from=%s to=executing (only failed Tasks can re-apply)", ErrInvalidTransition, task.ID, task.Status)
		}
		update := tx.Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, expectedVersion, "failed").
			Updates(map[string]any{"status": "executing", "version": gorm.Expr("version + 1")})
		if update.Error != nil {
			return fmt.Errorf("claim execution Task id=%d for re-apply: %w", task.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, expectedVersion)
		}
		newVersion = task.Version + 1
		fromStatus := "failed"
		if err := progress.AppendTaskEvent(tx, progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: newVersion, EventType: "reapply_started",
			FromStatus: &fromStatus, ToStatus: "executing", ActorType: "user",
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newVersion, nil
}

// LastApprovedProposal recovers the proposal a human approved for a Task by
// reading its execution_run audit history: the propose-stage run stored the full
// proposal (needs_approval=true) in output. It returns the newest such proposal
// so a re-apply lands exactly what was approved. Returns (nil, nil) when no
// approved proposal exists (e.g. the Task never went through approval).
func (s *Store) LastApprovedProposal(ctx context.Context, taskID uint64) (*codexProposal, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: Task ID is invalid", ErrInvalidInput)
	}
	var rows []domain.ExecutionRun
	if err := s.db.WithContext(ctx).
		Where("task_id = ? AND status = ?", taskID, "succeeded").
		Order("started_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load runs for approved proposal task_id=%d: %w", taskID, err)
	}
	for i := range rows {
		if proposal := proposalFromRunOutput(rows[i].Output); proposal != nil {
			return proposal, nil
		}
	}
	return nil, nil
}

// proposalFromRunOutput extracts a complete approved proposal from a propose-stage
// run's output (needs_approval=true + full proposal). It returns nil for any run
// whose output is not an approvable proposal (missing/partial), so callers can
// scan run history newest-first and take the first non-nil.
func proposalFromRunOutput(output []byte) *codexProposal {
	if len(output) == 0 {
		return nil
	}
	var out struct {
		NeedsApproval bool           `json:"needs_approval"`
		Proposal      *codexProposal `json:"proposal"`
	}
	if err := json.Unmarshal(output, &out); err != nil {
		return nil
	}
	if !out.NeedsApproval || out.Proposal == nil {
		return nil
	}
	if strings.TrimSpace(out.Proposal.Action) == "" ||
		strings.TrimSpace(out.Proposal.Target) == "" ||
		strings.TrimSpace(out.Proposal.Artifact) == "" {
		return nil
	}
	return out.Proposal
}

// ListRuns returns a Task's execution audit history, newest first. It is the
// read path over execution_run (previously write-only) that powers the task
// detail drawer. An unknown task_id simply yields an empty list.
func (s *Store) ListRuns(ctx context.Context, taskID uint64) (*RunList, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: Task ID is invalid", ErrInvalidInput)
	}
	var rows []domain.ExecutionRun
	if err := s.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("started_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list execution runs task_id=%d: %w", taskID, err)
	}
	items := make([]RunView, len(rows))
	for i := range rows {
		items[i] = runView(&rows[i])
	}
	return &RunList{Items: items}, nil
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

// FailStaleExecuting marks Tasks stuck in executing longer than olderThan as
// failed. This recovers zombies left when the process restarts mid-run (Kick*
// background goroutine dies but status stays executing). It also fails any
// orphaned execution_run still marked running for those Tasks. Uses updated_at
// as the "entered executing" clock (MarkExecuting bumps it).
func (s *Store) FailStaleExecuting(ctx context.Context, olderThan time.Duration, now time.Time) (int, error) {
	if olderThan <= 0 {
		return 0, fmt.Errorf("%w: stale executing threshold must be positive", ErrInvalidInput)
	}
	if now.IsZero() {
		return 0, fmt.Errorf("%w: stale executing now is required", ErrInvalidInput)
	}
	cutoff := now.UTC().Add(-olderThan)
	resultJSON, err := json.Marshal(map[string]any{
		"stage": "stale",
		"error": fmt.Sprintf("stale executing: stuck beyond %s (likely process restart killed background run)", olderThan.Round(time.Minute)),
	})
	if err != nil {
		return 0, fmt.Errorf("encode stale execution result: %w", err)
	}

	var staleTasks []domain.Task
	if err := s.db.WithContext(ctx).Model(&domain.Task{}).
		Select("id", "version").
		Where("status = ? AND updated_at < ?", "executing", cutoff).
		Find(&staleTasks).Error; err != nil {
		return 0, fmt.Errorf("list stale executing Tasks: %w", err)
	}
	if len(staleTasks) == 0 {
		return 0, nil
	}
	ids := make([]uint64, len(staleTasks))
	for i := range staleTasks {
		ids[i] = staleTasks[i].ID
	}

	errDetail := fmt.Sprintf("stale executing: stuck beyond %s", olderThan.Round(time.Minute))
	finishedAt := now.UTC()
	if err := s.db.WithContext(ctx).Model(&domain.ExecutionRun{}).
		Where("task_id IN ? AND status = ?", ids, "running").
		Updates(map[string]any{
			"status": "failed", "error_detail": errDetail, "finished_at": finishedAt,
		}).Error; err != nil {
		return 0, fmt.Errorf("fail stale execution runs: %w", err)
	}

	failed := 0
	fromStatus := "executing"
	for i := range staleTasks {
		task := &staleTasks[i]
		update := s.db.WithContext(ctx).Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, task.Version, "executing").
			Updates(map[string]any{
				"status": "failed", "execution_result": datatypes.JSON(resultJSON), "version": gorm.Expr("version + 1"),
			})
		if update.Error != nil {
			return failed, fmt.Errorf("fail stale executing Task id=%d: %w", task.ID, update.Error)
		}
		if update.RowsAffected == 0 {
			continue
		}
		if err := progress.AppendTaskEvent(s.db.WithContext(ctx), progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: task.Version + 1, EventType: "stale_failed",
			FromStatus: &fromStatus, ToStatus: "failed", ActorType: "system",
			Detail: map[string]any{"error": errDetail}, OccurredAt: finishedAt,
		}); err != nil {
			return failed, err
		}
		failed++
	}
	return failed, nil
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
		Target:     task.Target,
		Background: rawJSON(task.Background), Plan: rawJSON(task.Plan),
		ConfirmedBy: task.ConfirmedBy, ConfirmedAt: task.ConfirmedAt,
		SourceType: task.SourceType, SourceID: task.SourceID, OccurrenceKey: task.OccurrenceKey,
		ExecutionMode: task.ExecutionMode,
		Status:        task.Status, ExecutionResult: rawJSON(task.ExecutionResult),
		Summary: task.Summary, LastProgressAt: task.LastProgressAt,
		ExecutionSupplements: supplements,
		ProjectID:            task.ProjectID, Version: task.Version, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
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
