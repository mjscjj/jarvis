package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/gorm"
)

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
		err := tx.First(&task, taskID).Error
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
// is real but asks nothing of anyone. Leaving the clue on "materialized" would keep
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
	if err := db.First(&todo, *task.TodoID).Error; err != nil {
		return fmt.Errorf("lock clue id=%d for observing task_id=%d: %w", *task.TodoID, task.ID, err)
	}
	// A re-run of an already-parked Task lands here a second time.
	if todo.Status == "observing" {
		return nil
	}
	if todo.Status != "materialized" {
		return fmt.Errorf("%w: todo_id=%d from=%s to=observing", ErrInvalidTransition, todo.ID, todo.Status)
	}
	update := db.Model(&domain.Todo{}).
		Where("id = ? AND status = ?", todo.ID, "materialized").
		Updates(map[string]any{"status": "observing", "version": gorm.Expr("version + 1")})
	if update.Error != nil {
		return fmt.Errorf("park clue id=%d as observing: %w", todo.ID, update.Error)
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("%w: todo_id=%d from=materialized to=observing", ErrVersionConflict, todo.ID)
	}
	return createTodoEvent(db, todo.ID, "materialized", "observing", "m5", map[string]any{
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
		Where("NOT EXISTS (SELECT 1 FROM execution_run WHERE task_id = ? AND id > ?)", taskID, sourceRunID).
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

// ResetForRerun transitions a terminal Task (done/failed/observing) back to pending so it
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
		err := tx.First(&task, taskID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		if err != nil {
			return fmt.Errorf("lock execution Task id=%d: %w", taskID, err)
		}
		reset, err := resetTaskForRerun(tx, &task, "user", nil, time.Now())
		if err != nil {
			return err
		}
		reloaded = *reset
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &reloaded, nil
}

// resetTaskForRerun is the shared persistence boundary for both a principal
// rerun and an automatic rerun caused by fresh evidence on an observing Todo.
// The Task's source_payload stays frozen; prior runs plus live tools give M5
// the history and current-world lookup path for the new run.
func resetTaskForRerun(db *gorm.DB, task *domain.Task, actorType string, detail any, occurredAt time.Time) (*domain.Task, error) {
	if db == nil || task == nil || task.ID == 0 || occurredAt.IsZero() {
		return nil, fmt.Errorf("%w: rerun Task, db and occurred_at are required", ErrInvalidInput)
	}
	// observing reruns like any other terminal state: "nobody needs to act"
	// was a verdict on the evidence at the time, and new evidence can overturn
	// it. The source Todo is moved back to materialized by the caller that owns it.
	if task.Status != "done" && task.Status != "failed" && task.Status != "observing" {
		return nil, fmt.Errorf("%w: task_id=%d from=%s to=pending (only finished Tasks can rerun)", ErrInvalidTransition, task.ID, task.Status)
	}
	update := db.Model(&domain.Task{}).
		Where("id = ? AND version = ? AND status = ?", task.ID, task.Version, task.Status).
		Updates(map[string]any{
			"status": "pending", "execution_result": nil,
			"execution_supplements": task.ExecutionSupplements,
			"version":               gorm.Expr("version + 1"),
		})
	if update.Error != nil {
		return nil, fmt.Errorf("reset execution Task id=%d for rerun: %w", task.ID, update.Error)
	}
	if update.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: task_id=%d", ErrVersionConflict, task.ID)
	}
	newVersion := task.Version + 1
	fromStatus := task.Status
	if err := progress.AppendTaskEvent(db, progress.TaskEventInput{
		TaskID: task.ID, TaskVersion: newVersion, EventType: "rerun_requested",
		FromStatus: &fromStatus, ToStatus: "pending", ActorType: actorType,
		Detail: detail, OccurredAt: occurredAt.UTC(),
	}); err != nil {
		return nil, err
	}
	var reloaded domain.Task
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		return nil, fmt.Errorf("reload execution Task id=%d after reset: %w", task.ID, err)
	}
	return &reloaded, nil
}

// LoadPending returns pending Tasks for the scheduled executor, oldest first.
func (s *Store) LoadPending(ctx context.Context, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: pending load limit must be positive", ErrInvalidInput)
	}
	var rows []domain.Task
	if err := s.db.WithContext(ctx).
		Where("status = ?", "pending").
		Order("created_at ASC, id ASC").
		Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load pending execution Tasks: %w", err)
	}
	return rows, nil
}

// StaleSweep counts what one stale-executing sweep did.
type StaleSweep struct {
	// Requeued is Tasks put back in the queue because no run ever started for
	// them, so nothing they could have done reached the outside world.
	Requeued int
	// Failed is Tasks whose agent did start and may already have written to the
	// outside world. Re-running those could repeat a message or a merge request,
	// so they stop here and wait for a human.
	Failed int
}

// FailStaleExecuting recovers Tasks stuck in executing longer than olderThan,
// the zombies left when the process dies mid-run: the background goroutine is
// gone but the status never moved, and nothing else looks at these Tasks again.
//
// Whether a zombie is safe to re-run comes down to whether its agent ever
// started, which is what the execution_run row records — SaveRun lands it as
// running before the agent is invoked. No row means no side effects were
// possible, so the Task goes back to pending; a row means the opposite, so the
// Task fails and the orphaned run is closed out with it. Uses updated_at as the
// "entered executing" clock (MarkExecuting bumps it).
func (s *Store) FailStaleExecuting(ctx context.Context, olderThan time.Duration, now time.Time) (StaleSweep, error) {
	var sweep StaleSweep
	if olderThan <= 0 {
		return sweep, fmt.Errorf("%w: stale executing threshold must be positive", ErrInvalidInput)
	}
	if now.IsZero() {
		return sweep, fmt.Errorf("%w: stale executing now is required", ErrInvalidInput)
	}
	cutoff := now.UTC().Add(-olderThan)
	errDetail := fmt.Sprintf("stale executing: stuck beyond %s", olderThan.Round(time.Minute))
	resultJSON, err := json.Marshal(map[string]any{
		"stage": "stale",
		"error": errDetail + " (likely process restart killed background run)",
	})
	if err != nil {
		return sweep, fmt.Errorf("encode stale execution result: %w", err)
	}

	var staleTasks []domain.Task
	if err := s.db.WithContext(ctx).Model(&domain.Task{}).
		Select("id", "version").
		Where("status = ? AND datetime(updated_at) < datetime(?)", "executing", cutoff.Format(time.RFC3339Nano)).
		Find(&staleTasks).Error; err != nil {
		return sweep, fmt.Errorf("list stale executing Tasks: %w", err)
	}
	if len(staleTasks) == 0 {
		return sweep, nil
	}
	ids := make([]uint64, len(staleTasks))
	for i := range staleTasks {
		ids[i] = staleTasks[i].ID
	}
	var startedIDs []uint64
	if err := s.db.WithContext(ctx).Model(&domain.ExecutionRun{}).
		Where("task_id IN ?", ids).Distinct().Pluck("task_id", &startedIDs).Error; err != nil {
		return sweep, fmt.Errorf("list stale Tasks with started runs: %w", err)
	}
	started := make(map[uint64]struct{}, len(startedIDs))
	for _, id := range startedIDs {
		started[id] = struct{}{}
	}

	finishedAt := now.UTC()
	if len(startedIDs) > 0 {
		if err := s.db.WithContext(ctx).Model(&domain.ExecutionRun{}).
			Where("task_id IN ? AND status = ?", startedIDs, "running").
			Updates(map[string]any{
				"status": "failed", "error_detail": errDetail, "finished_at": finishedAt,
			}).Error; err != nil {
			return sweep, fmt.Errorf("fail stale execution runs: %w", err)
		}
	}

	fromStatus := "executing"
	for i := range staleTasks {
		task := &staleTasks[i]
		_, agentStarted := started[task.ID]
		changes := map[string]any{"status": "pending", "version": gorm.Expr("version + 1")}
		eventType, toStatus := "stale_requeued", "pending"
		if agentStarted {
			changes = map[string]any{
				"status": "failed", "execution_result": datatypes.JSON(resultJSON), "version": gorm.Expr("version + 1"),
			}
			eventType, toStatus = "stale_failed", "failed"
		}
		update := s.db.WithContext(ctx).Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, task.Version, "executing").
			Updates(changes)
		if update.Error != nil {
			return sweep, fmt.Errorf("sweep stale executing Task id=%d: %w", task.ID, update.Error)
		}
		if update.RowsAffected == 0 {
			continue
		}
		if err := progress.AppendTaskEvent(s.db.WithContext(ctx), progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: task.Version + 1, EventType: eventType,
			FromStatus: &fromStatus, ToStatus: toStatus, ActorType: "system",
			Detail: map[string]any{"error": errDetail}, OccurredAt: finishedAt,
		}); err != nil {
			return sweep, err
		}
		if agentStarted {
			sweep.Failed++
		} else {
			sweep.Requeued++
		}
	}
	return sweep, nil
}
