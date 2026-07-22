// Package execute owns the MVP Task execution lifecycle.
package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	"pending": {}, "executing": {}, "awaiting_approval": {}, "done": {}, "failed": {},
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
	ID                   uint64                `json:"id"`
	TodoID               uint64                `json:"todo_id"`
	Title                string                `json:"title"`
	ActionType           string                `json:"action_type"`
	Background           json.RawMessage       `json:"background"`
	Plan                 json.RawMessage       `json:"plan"`
	ConfirmedBy          string                `json:"confirmed_by"`
	ConfirmedAt          time.Time             `json:"confirmed_at"`
	ActionHash           string                `json:"action_hash"`
	Status               string                `json:"status"`
	ExecutionResult      json.RawMessage       `json:"execution_result"`
	ExecutionSupplements []ExecutionSupplement `json:"execution_supplements,omitempty"`
	AutonomyMode         string                `json:"autonomy_mode"`
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
}

type SupplementInput struct {
	TaskID          uint64
	ExpectedVersion int32
	Note            string
	Channel         string
}

// RunView 是一次 ExecutionRun 审计记录的只读视图，供任务详情展示执行历史。
// Prompt 全文可能很大，这里不透出，只给结构化产物与状态/耗时。
type RunView struct {
	ID              uint64          `json:"id"`
	TaskID          uint64          `json:"task_id"`
	ActionType      string          `json:"action_type"`
	Sandbox         string          `json:"sandbox"`
	Status          string          `json:"status"`
	CodexSessionID  *string         `json:"codex_session_id"`
	Summary         *string         `json:"summary"`
	Output          json.RawMessage `json:"output"`
	ErrorDetail     *string         `json:"error_detail"`
	RepoPath        *string         `json:"repo_path"`
	Branch          *string         `json:"branch"`
	Commit          *string         `json:"commit"`
	DiffPath        *string         `json:"diff_path"`
	MergeRequestURL *string         `json:"merge_request_url"`
	StartedAt       time.Time       `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at"`
	DurationMs      *int64          `json:"duration_ms"`
}

type RunList struct {
	Items []RunView `json:"items"`
}

type TaskService interface {
	ListTasks(context.Context, TaskFilter) (*TaskList, error)
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

var supplementableTaskStatuses = map[string]struct{}{
	"pending": {}, "executing": {}, "awaiting_approval": {}, "done": {}, "failed": {},
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
	view := taskView(&reloaded)
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

// MarkAwaitingApproval parks an executing Task at awaiting_approval after the
// propose stage decided the external write is high-risk. It stores the approved-
// pending proposal (the plan + full artifact codex produced without touching the
// outside world) into execution_result so the UI can render it and the later
// apply stage can replay it. It bumps the version and returns the new version.
func (s *Store) MarkAwaitingApproval(ctx context.Context, taskID uint64, expectedVersion int32, proposal json.RawMessage) (int32, error) {
	if taskID == 0 || expectedVersion < 0 {
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
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newVersion, nil
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
		if err := tx.First(&rejected, taskID).Error; err != nil {
			return fmt.Errorf("reload execution Task id=%d after reject: %w", task.ID, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	view := taskView(&rejected)
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
		if task.Status != "done" && task.Status != "failed" {
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

	var ids []uint64
	if err := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("status = ? AND updated_at < ?", "executing", cutoff).
		Pluck("id", &ids).Error; err != nil {
		return 0, fmt.Errorf("list stale executing Tasks: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
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

	update := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("id IN ? AND status = ?", ids, "executing").
		Updates(map[string]any{
			"status": "failed", "execution_result": datatypes.JSON(resultJSON), "version": gorm.Expr("version + 1"),
		})
	if update.Error != nil {
		return 0, fmt.Errorf("fail stale executing Tasks: %w", update.Error)
	}
	return int(update.RowsAffected), nil
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
	supplements, err := decodeExecutionSupplements(task.ExecutionSupplements)
	if err != nil {
		// 写入侧 Supplement 已严格校验，正常不会存进坏数据；一旦解析失败说明库里
		// 的 execution_supplements 被损坏。这里 taskView 无法返回 error，至少打点
		// 暴露问题（不静默吞掉，符合 fail-fast），补充信息在本次视图中缺省为空。
		log.Printf("taskView: decode execution_supplements task_id=%d failed: %v", task.ID, err)
		supplements = nil
	}
	return TaskView{
		ID: task.ID, TodoID: task.TodoID, Title: task.Title, ActionType: task.ActionType,
		Background: rawJSON(task.Background), Plan: rawJSON(task.Plan),
		ConfirmedBy: task.ConfirmedBy, ConfirmedAt: task.ConfirmedAt, ActionHash: task.ActionHash,
		Status: task.Status, ExecutionResult: rawJSON(task.ExecutionResult),
		ExecutionSupplements: supplements,
		AutonomyMode:         task.AutonomyMode,
		ProjectID:            task.ProjectID, Version: task.Version, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

func runView(run *domain.ExecutionRun) RunView {
	return RunView{
		ID: run.ID, TaskID: run.TaskID, ActionType: run.ActionType, Sandbox: run.Sandbox,
		Status: run.Status, CodexSessionID: run.CodexSessionID, Summary: run.Summary,
		Output: rawJSON(run.Output), ErrorDetail: run.ErrorDetail,
		RepoPath: run.RepoPath, Branch: run.Branch, Commit: run.Commit,
		DiffPath: run.DiffPath, MergeRequestURL: run.MergeRequestURL,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, DurationMs: run.DurationMs,
	}
}

func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), value...))
}
