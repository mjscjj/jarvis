// Package scheduledtask implements durable time triggers backed by SQLite.
package scheduledtask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/observability"
	"jarvis/internal/taskcreate"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gorm.io/gorm"
	"jarvis/internal/datatypes"
)

var (
	ErrInvalidInput = errors.New("invalid scheduled task input")
	ErrNotFound     = errors.New("scheduled task not found")
	ErrRunning      = errors.New("scheduled task is running")
)

var validStatuses = map[string]struct{}{"binding": {}, "active": {}, "running": {}, "completed": {}}

type Input struct {
	DispatchKind    string          `json:"dispatch_kind"`
	SubjectType     *string         `json:"subject_type"`
	SubjectID       *uint64         `json:"subject_id"`
	SourceRunID     *uint64         `json:"source_run_id"`
	DispatchPayload json.RawMessage `json:"dispatch_payload"`
	Title           string          `json:"title"`
	ActionType      string          `json:"action_type"`
	Instruction     string          `json:"instruction"`
	ContextSnapshot json.RawMessage `json:"context_snapshot"`
	ScheduleType    string          `json:"schedule_type"`
	DailyTime       *string         `json:"daily_time"`
	Weekday         *int            `json:"weekday"`
	IntervalMinutes *int            `json:"interval_minutes"`
	RunAt           *time.Time      `json:"run_at"`
	Enabled         *bool           `json:"enabled"`
	initialStatus   string
}

type View struct {
	ID              uint64          `json:"id"`
	DispatchKind    string          `json:"dispatch_kind"`
	SubjectType     *string         `json:"subject_type"`
	SubjectID       *uint64         `json:"subject_id"`
	SourceRunID     *uint64         `json:"source_run_id"`
	DispatchPayload json.RawMessage `json:"dispatch_payload"`
	Title           string          `json:"title"`
	ActionType      string          `json:"action_type"`
	Instruction     string          `json:"instruction"`
	ContextSnapshot json.RawMessage `json:"context_snapshot"`
	ScheduleType    string          `json:"schedule_type"`
	DailyTime       *string         `json:"daily_time"`
	Weekday         *int            `json:"weekday"`
	IntervalMinutes *int            `json:"interval_minutes"`
	RunAt           *time.Time      `json:"run_at"`
	NextRunAt       time.Time       `json:"next_run_at"`
	Enabled         bool            `json:"enabled"`
	Status          string          `json:"status"`
	LastRunStatus   *string         `json:"last_run_status"`
	LastTaskID      *uint64         `json:"last_task_id"`
	LastResult      *string         `json:"last_result"`
	LastErrorDetail *string         `json:"last_error_detail"`
	LastStartedAt   *time.Time      `json:"last_started_at"`
	LastFinishedAt  *time.Time      `json:"last_finished_at"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type ListFilter struct {
	Status   string
	Limit    int
	PluginID string
}

type TaskSubmitter interface {
	Submit(context.Context, taskcreate.Input) (*domain.Task, error)
}

type TaskResumer interface {
	ResumeTask(context.Context, uint64, uint64, string) error
}

type YieldInput struct {
	TaskID uint64
	RunAt  time.Time
	Reason string
}

type Service struct {
	db         *gorm.DB
	submitter  TaskSubmitter
	resumer    TaskResumer
	batchLimit int
	now        func() time.Time
	location   *time.Location
	moduleGate func(context.Context, string) (bool, error)
	pluginGate func(context.Context, string) (bool, error)
	skillGate  func(context.Context, string) (bool, error)
}

// SetPluginGate suspends new dispatches for schedules owned by disabled plugins.
// Existing Tasks and their continuation schedules keep their lifecycle.
func (s *Service) SetPluginGate(gate func(context.Context, string) (bool, error)) {
	s.pluginGate = gate
}

func (s *Service) SetSkillGate(gate func(context.Context, string) (bool, error)) {
	s.skillGate = gate
}

// SetModuleGate makes module-owned schedules dormant while their module is
// disabled. ContextSnapshot opts in with a top-level `module` string; ordinary
// schedules remain unaffected.
func (s *Service) SetModuleGate(gate func(context.Context, string) (bool, error)) {
	s.moduleGate = gate
}

// NewCRUDService constructs the storage-only surface used by jarvis-tools.
// Execution methods still fail fast if called without the full NewService.
func NewCRUDService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("scheduled task db is nil")
	}
	return &Service{db: db, now: time.Now, location: time.Local}, nil
}

func NewService(db *gorm.DB, submitter TaskSubmitter, resumer TaskResumer, batchLimit int, location *time.Location) (*Service, error) {
	service, err := NewCRUDService(db)
	if err != nil {
		return nil, err
	}
	if submitter == nil {
		return nil, fmt.Errorf("scheduled task Task submitter is nil")
	}
	if resumer == nil {
		return nil, fmt.Errorf("scheduled task Task resumer is nil")
	}
	if batchLimit <= 0 {
		return nil, fmt.Errorf("scheduled task batch limit must be positive")
	}
	if location == nil {
		return nil, fmt.Errorf("scheduled task location is nil")
	}
	service.submitter = submitter
	service.resumer = resumer
	service.batchLimit = batchLimit
	service.location = location
	return service, nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]View, error) {
	if filter.Limit <= 0 || filter.Limit > 500 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 500", ErrInvalidInput)
	}
	query := s.db.WithContext(ctx).Model(&domain.ScheduledTask{})
	if pluginID := strings.TrimSpace(filter.PluginID); pluginID != "" {
		query = query.Where("json_extract(context_snapshot, '$.plugin') = ?", pluginID)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		if _, ok := validStatuses[status]; !ok {
			return nil, fmt.Errorf("%w: unknown status %q", ErrInvalidInput, status)
		}
		query = query.Where("status = ?", status)
	}
	var rows []domain.ScheduledTask
	if err := query.Order("enabled DESC, next_run_at ASC, id ASC").Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list scheduled tasks: %w", err)
	}
	views := make([]View, len(rows))
	for i := range rows {
		views[i] = toView(&rows[i])
	}
	return views, nil
}

func (s *Service) Get(ctx context.Context, id uint64) (*View, error) {
	row, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	view := toView(row)
	return &view, nil
}

func (s *Service) Create(ctx context.Context, input Input) (*View, error) {
	normalized, nextRunAt, err := normalizeInput(input, s.now(), s.location)
	if err != nil {
		return nil, err
	}
	row := domain.ScheduledTask{
		DispatchKind: normalized.DispatchKind, SubjectType: normalized.SubjectType,
		SubjectID: normalized.SubjectID, SourceRunID: normalized.SourceRunID,
		DispatchPayload: datatypes.JSON(normalized.DispatchPayload),
		Title:           normalized.Title, ActionType: normalized.ActionType, Instruction: normalized.Instruction,
		ContextSnapshot: datatypes.JSON(normalized.ContextSnapshot),
		ScheduleType:    normalized.ScheduleType, DailyTime: normalized.DailyTime, Weekday: normalized.Weekday,
		IntervalMinutes: normalized.IntervalMinutes, RunAt: normalized.RunAt, NextRunAt: nextRunAt,
		Enabled: *normalized.Enabled, Status: normalized.initialStatus,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("create scheduled task: %w", err)
	}
	return s.Get(ctx, row.ID)
}

func (s *Service) CreateYield(ctx context.Context, input YieldInput) (*View, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.TaskID == 0 || input.RunAt.IsZero() || input.Reason == "" {
		return nil, fmt.Errorf("%w: yield requires task_id, run_at and reason", ErrInvalidInput)
	}
	now := s.now().UTC()
	runAt := input.RunAt.UTC()
	if !runAt.After(now) {
		return nil, fmt.Errorf("%w: yield run_at must be in the future", ErrInvalidInput)
	}
	var task domain.Task
	if err := s.db.WithContext(ctx).First(&task, input.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrNotFound, input.TaskID)
		}
		return nil, fmt.Errorf("load yielding Task id=%d: %w", input.TaskID, err)
	}
	if task.Status != "executing" {
		return nil, fmt.Errorf("%w: task_id=%d status=%s cannot yield", ErrInvalidInput, task.ID, task.Status)
	}
	var existing int64
	if err := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("dispatch_kind = ? AND subject_type = ? AND subject_id = ? AND status IN ?",
			"resume_task", "task", task.ID, []string{"binding", "active"}).
		Count(&existing).Error; err != nil {
		return nil, fmt.Errorf("check existing yield for task_id=%d: %w", task.ID, err)
	}
	if existing != 0 {
		return nil, fmt.Errorf("%w: task_id=%d already has a pending continuation schedule", ErrInvalidInput, task.ID)
	}
	subjectType := "task"
	enabled := true
	payload, err := json.Marshal(map[string]any{"reason": input.Reason})
	if err != nil {
		return nil, fmt.Errorf("encode yield payload: %w", err)
	}
	return s.Create(ctx, Input{
		DispatchKind: "resume_task", SubjectType: &subjectType, SubjectID: &input.TaskID,
		DispatchPayload: payload,
		Title:           fmt.Sprintf("继续 Task #%d：%s", task.ID, task.Title), ActionType: task.ActionType,
		Instruction: input.Reason, ContextSnapshot: json.RawMessage(`{}`),
		ScheduleType: "once", RunAt: &runAt, Enabled: &enabled, initialStatus: "binding",
	})
}

func (s *Service) Update(ctx context.Context, id uint64, input Input) (*View, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	existing, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.DispatchKind == "resume_task" {
		return nil, fmt.Errorf("%w: continuation schedule id=%d is owned by its waiting Task and cannot be edited", ErrInvalidInput, id)
	}
	normalized, nextRunAt, err := normalizeInput(input, s.now(), s.location)
	if err != nil {
		return nil, err
	}
	result := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status <> ?", id, "running").
		Updates(map[string]any{
			"title": normalized.Title, "action_type": normalized.ActionType, "instruction": normalized.Instruction,
			"context_snapshot": datatypes.JSON(normalized.ContextSnapshot),
			"schedule_type":    normalized.ScheduleType, "daily_time": normalized.DailyTime, "weekday": normalized.Weekday,
			"interval_minutes": normalized.IntervalMinutes, "run_at": normalized.RunAt, "next_run_at": nextRunAt,
			"enabled": *normalized.Enabled, "status": "active",
		})
	if result.Error != nil {
		return nil, fmt.Errorf("update scheduled task id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		row, loadErr := s.load(ctx, id)
		if loadErr != nil {
			return nil, loadErr
		}
		if row.Status == "running" {
			return nil, ErrRunning
		}
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	row, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	if err := validateDeletable(row); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).
		Where("id = ? AND status <> ? AND dispatch_kind <> ?", id, "running", "resume_task").
		Delete(&domain.ScheduledTask{})
	if result.Error != nil {
		return fmt.Errorf("delete scheduled task id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 0 {
		return nil
	}
	row, err = s.load(ctx, id)
	if err != nil {
		return err
	}
	if err := validateDeletable(row); err != nil {
		return err
	}
	return fmt.Errorf("delete scheduled task id=%d affected no rows", id)
}

func validateDeletable(row *domain.ScheduledTask) error {
	if row.DispatchKind == "resume_task" {
		return fmt.Errorf("%w: continuation schedule id=%d is owned by its waiting Task and cannot be deleted", ErrInvalidInput, row.ID)
	}
	if row.Status == "running" {
		return ErrRunning
	}
	return nil
}

// Trigger materializes and dispatches a Task immediately. Recurring schedules
// keep their next automatic occurrence; a one-time schedule becomes completed.
func (s *Service) Trigger(ctx context.Context, id uint64) (*View, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	if s.submitter == nil {
		return nil, fmt.Errorf("scheduled task Task submitter is not configured")
	}
	now := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status IN ?", id, []string{"active", "completed"}).
		Updates(map[string]any{
			"status": "running", "last_run_status": nil, "last_error_detail": nil,
			"last_started_at": now, "last_finished_at": nil,
		})
	if result.Error != nil {
		return nil, fmt.Errorf("claim scheduled task id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		row, loadErr := s.load(ctx, id)
		if loadErr != nil {
			return nil, loadErr
		}
		if row.Status == "running" {
			return nil, ErrRunning
		}
		return nil, fmt.Errorf("claim scheduled task id=%d affected no rows", id)
	}
	row, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	occurrenceKey := "manual:" + now.Format(time.RFC3339Nano)
	if err := s.dispatch(observability.Detached(ctx), row, occurrenceKey); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// RecoverRunning is called once at process startup. A schedule left in running
// did not finish materializing its Task and must become claimable again.
func (s *Service) RecoverRunning(ctx context.Context) (int64, error) {
	now := s.now().UTC()
	binding := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("status = ?", "binding").Updates(map[string]any{
		"status": "completed", "last_run_status": "failed", "last_finished_at": now,
		"last_error_detail": "yield binding interrupted by Jarvis process restart",
	})
	if binding.Error != nil {
		return 0, fmt.Errorf("recover binding scheduled tasks: %w", binding.Error)
	}
	recurring := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("status = ? AND schedule_type <> ?", "running", "once").Updates(map[string]any{
		"status": "active", "last_run_status": "failed", "last_finished_at": now,
		"last_error_detail": "recovered after Jarvis process restart",
	})
	if recurring.Error != nil {
		return 0, fmt.Errorf("recover running scheduled tasks: %w", recurring.Error)
	}
	oneTime := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("status = ? AND schedule_type = ?", "running", "once").Updates(map[string]any{
		"status": "completed", "last_run_status": "failed", "last_finished_at": now,
		"last_error_detail": "recovered after Jarvis process restart",
	})
	if oneTime.Error != nil {
		return 0, fmt.Errorf("recover running one-time scheduled tasks: %w", oneTime.Error)
	}
	return binding.RowsAffected + recurring.RowsAffected + oneTime.RowsAffected, nil
}

// RunDue claims one bounded batch and materializes each occurrence as a Task.
// M5 owns execution concurrency and recovery after the Task is durable.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	if s.submitter == nil || s.batchLimit <= 0 {
		return 0, fmt.Errorf("scheduled task Task submitter is not configured")
	}
	claimed, err := s.claimDue(ctx, s.now().UTC())
	if err != nil || len(claimed) == 0 {
		return len(claimed), err
	}
	var firstErr error
	for i := range claimed {
		if err := s.dispatch(ctx, &claimed[i], claimed[i].NextRunAt.UTC().Format(time.RFC3339Nano)); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return len(claimed), firstErr
}

func (s *Service) claimDue(ctx context.Context, now time.Time) ([]domain.ScheduledTask, error) {
	var candidates []domain.ScheduledTask
	if err := s.db.WithContext(ctx).
		Where("enabled = ? AND status = ? AND next_run_at <= ?", true, "active", now).
		Order("next_run_at ASC, id ASC").Limit(s.batchLimit).Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("load due scheduled tasks: %w", err)
	}
	claimed := make([]domain.ScheduledTask, 0, len(candidates))
	for i := range candidates {
		updates := map[string]any{
			"status": "running", "last_run_status": nil, "last_error_detail": nil,
			"last_started_at": s.now().UTC(), "last_finished_at": nil,
		}
		if candidates[i].ScheduleType != "once" {
			nextRunAt, err := nextOccurrence(&candidates[i], now, s.location)
			if err != nil {
				return claimed, fmt.Errorf("scheduled task id=%d compute next run: %w", candidates[i].ID, err)
			}
			updates["next_run_at"] = nextRunAt
		}
		result := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
			Where("id = ? AND enabled = ? AND status = ? AND next_run_at = ?", candidates[i].ID, true, "active", candidates[i].NextRunAt).
			Updates(updates)
		if result.Error != nil {
			return claimed, fmt.Errorf("claim due scheduled task id=%d: %w", candidates[i].ID, result.Error)
		}
		if result.RowsAffected == 1 {
			claimed = append(claimed, candidates[i])
		}
	}
	return claimed, nil
}

func (s *Service) dispatch(ctx context.Context, row *domain.ScheduledTask, occurrenceKey string) error {
	ctx = observability.Detached(ctx)
	if row == nil || row.ID == 0 {
		return fmt.Errorf("scheduled task dispatch row is invalid")
	}
	if row.DispatchKind == "resume_task" {
		return s.dispatchResume(ctx, row)
	}
	if row.DispatchKind != "create_task" {
		err := fmt.Errorf("scheduled task id=%d has unknown dispatch_kind=%q", row.ID, row.DispatchKind)
		s.fail(ctx, row.ID, err)
		return err
	}
	moduleKey, err := scheduledModuleKey(row.ContextSnapshot)
	if err != nil {
		s.fail(ctx, row.ID, err)
		return err
	}
	if moduleKey != "" && s.moduleGate != nil {
		enabled, err := s.moduleGate(ctx, moduleKey)
		if err != nil {
			s.fail(ctx, row.ID, err)
			return err
		}
		if !enabled {
			return s.skipDisabledModule(ctx, row, moduleKey)
		}
	}
	pluginID, err := scheduledContextKey(row.ContextSnapshot, "plugin")
	if err != nil {
		s.fail(ctx, row.ID, err)
		return err
	}
	if pluginID != "" && s.pluginGate != nil {
		enabled, err := s.pluginGate(ctx, pluginID)
		if err != nil {
			s.fail(ctx, row.ID, err)
			return err
		}
		if !enabled {
			return s.skipDisabledOwner(ctx, row, "插件 "+pluginID)
		}
	}
	skillName, err := scheduledContextKey(row.ContextSnapshot, "skill")
	if err != nil {
		s.fail(ctx, row.ID, err)
		return err
	}
	if skillName != "" && s.skillGate != nil {
		enabled, err := s.skillGate(ctx, skillName)
		if err != nil {
			s.fail(ctx, row.ID, err)
			return err
		}
		if !enabled {
			return s.skipDisabledOwner(ctx, row, "Skill "+skillName)
		}
	}
	input, err := taskInput(row, occurrenceKey)
	if err != nil {
		s.fail(ctx, row.ID, err)
		return err
	}
	task, err := s.submitter.Submit(ctx, input)
	if err != nil {
		s.fail(ctx, row.ID, err)
		return err
	}
	finishedAt := s.now().UTC()
	finalStatus := finalTaskStatus(row.ScheduleType)
	result := fmt.Sprintf("已创建并提交 Task #%d；实际执行结果以该 Task 为准", task.ID)
	update := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status = ?", row.ID, "running").
		Updates(map[string]any{
			"status": finalStatus, "last_run_status": "done", "last_result": result,
			"last_task_id": task.ID, "last_error_detail": nil, "last_finished_at": finishedAt,
		})
	if update.Error != nil {
		s.fail(ctx, row.ID, fmt.Errorf("store scheduled task trigger result: %w", update.Error))
		return update.Error
	}
	return nil
}

func scheduledModuleKey(raw []byte) (string, error) {
	return scheduledContextKey(raw, "module")
}

func scheduledContextKey(raw []byte, key string) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return "", fmt.Errorf("decode scheduled task module context: %w", err)
	}
	encoded, ok := snapshot[key]
	if !ok {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return "", fmt.Errorf("scheduled task %s must be a string", key)
	}
	return strings.TrimSpace(value), nil
}

func (s *Service) skipDisabledModule(ctx context.Context, row *domain.ScheduledTask, moduleKey string) error {
	return s.skipDisabledOwner(ctx, row, "模块 "+moduleKey)
}

func (s *Service) skipDisabledOwner(ctx context.Context, row *domain.ScheduledTask, owner string) error {
	finishedAt := s.now().UTC()
	result := fmt.Sprintf("%s 已关闭，本轮未创建 Task", owner)
	update := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status = ?", row.ID, "running").
		Updates(map[string]any{
			"status": finalTaskStatus(row.ScheduleType), "last_run_status": "done",
			"last_result": result, "last_error_detail": nil, "last_finished_at": finishedAt,
		})
	if update.Error != nil {
		return fmt.Errorf("store disabled-module schedule result: %w", update.Error)
	}
	return nil
}

func (s *Service) dispatchResume(ctx context.Context, row *domain.ScheduledTask) error {
	if s.resumer == nil {
		err := fmt.Errorf("scheduled task Task resumer is not configured")
		s.fail(ctx, row.ID, err)
		return err
	}
	if row.SubjectType == nil || *row.SubjectType != "task" || row.SubjectID == nil || *row.SubjectID == 0 || row.SourceRunID == nil || *row.SourceRunID == 0 {
		err := fmt.Errorf("resume scheduled task id=%d is not bound to a Task and source run", row.ID)
		s.fail(ctx, row.ID, err)
		return err
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	if finished, err := s.finishTerminalResume(ctx, row); err != nil || finished {
		if err != nil {
			s.fail(ctx, row.ID, err)
		}
		return err
	}
	if err := json.Unmarshal(row.DispatchPayload, &payload); err != nil {
		cause := fmt.Errorf("decode resume scheduled task id=%d payload: %w", row.ID, err)
		s.fail(ctx, row.ID, cause)
		return cause
	}
	payload.Reason = strings.TrimSpace(payload.Reason)
	if payload.Reason == "" {
		err := fmt.Errorf("resume scheduled task id=%d reason is blank", row.ID)
		s.fail(ctx, row.ID, err)
		return err
	}
	if err := s.resumer.ResumeTask(ctx, *row.SubjectID, *row.SourceRunID, payload.Reason); err != nil {
		// Close can win the Task version check after this trigger was claimed.
		if finished, checkErr := s.finishTerminalResume(ctx, row); checkErr != nil || finished {
			if checkErr != nil {
				s.fail(ctx, row.ID, checkErr)
			}
			return checkErr
		}
		s.fail(ctx, row.ID, err)
		return err
	}
	finishedAt := s.now().UTC()
	result := fmt.Sprintf("已恢复 Task #%d 的 Codex Session", *row.SubjectID)
	update := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status = ?", row.ID, "running").
		Updates(map[string]any{
			"status": "completed", "last_run_status": "done", "last_result": result,
			"last_error_detail": nil, "last_finished_at": finishedAt,
		})
	if update.Error != nil {
		return fmt.Errorf("store resume scheduled task result: %w", update.Error)
	}
	return nil
}

// A trigger can outlive its Task when close cleanup was interrupted, or be
// claimed concurrently with close. Preserve that fact without starting M5.
func (s *Service) finishTerminalResume(ctx context.Context, row *domain.ScheduledTask) (bool, error) {
	var task domain.Task
	if err := s.db.WithContext(ctx).Select("id,status").First(&task, *row.SubjectID).Error; err != nil {
		return false, fmt.Errorf("check resume Task #%d: %w", *row.SubjectID, err)
	}
	if task.Status != "done" && task.Status != "failed" && task.Status != "observing" {
		return false, nil
	}
	result := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status = ?", row.ID, "running").
		Updates(map[string]any{
			"status": "completed", "last_run_status": "done", "last_error_detail": nil,
			"last_result":      fmt.Sprintf("Task #%d 已为 %s，恢复触发已失效，未启动执行", task.ID, task.Status),
			"last_finished_at": s.now().UTC(),
		})
	if result.Error != nil {
		return false, fmt.Errorf("finish obsolete resume #%d: %w", row.ID, result.Error)
	}
	return true, nil
}

func taskInput(row *domain.ScheduledTask, occurrenceKey string) (taskcreate.Input, error) {
	if row == nil || row.ID == 0 {
		return taskcreate.Input{}, fmt.Errorf("scheduled task row is invalid")
	}
	occurrenceKey = strings.TrimSpace(occurrenceKey)
	if occurrenceKey == "" {
		return taskcreate.Input{}, fmt.Errorf("scheduled task occurrence key is empty")
	}
	sourcePayload, err := json.Marshal(map[string]any{"instruction": row.Instruction})
	if err != nil {
		return taskcreate.Input{}, fmt.Errorf("encode scheduled Task source payload: %w", err)
	}
	return taskcreate.Input{
		Title: row.Title, ActionType: row.ActionType, Target: row.Title,
		Background: json.RawMessage(row.ContextSnapshot), SourcePayload: sourcePayload,
		SourceType: taskcreate.SourceScheduledTask,
		SourceID:   &row.ID, OccurrenceKey: &occurrenceKey,
		ActorType:   "scheduled_task",
		EventDetail: map[string]any{"scheduled_task_id": row.ID, "occurrence_key": occurrenceKey},
	}, nil
}

func (s *Service) fail(ctx context.Context, id uint64, cause error) {
	if cause == nil {
		return
	}
	finishedAt := s.now().UTC()
	row, loadErr := s.load(ctx, id)
	if loadErr != nil {
		hlog.CtxErrorf(ctx, "scheduled task failure persistence failed id=%d load_error=%+v original_error=%+v", id, loadErr, cause)
		return
	}
	if err := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]any{
			"status": finalTaskStatus(row.ScheduleType), "last_run_status": "failed",
			"last_error_detail": cause.Error(), "last_finished_at": finishedAt,
		}).Error; err != nil {
		hlog.CtxErrorf(ctx, "scheduled task failure persistence failed id=%d store_error=%+v original_error=%+v", id, err, cause)
	}
}

func finalTaskStatus(scheduleType string) string {
	if scheduleType == "once" {
		return "completed"
	}
	return "active"
}

func (s *Service) load(ctx context.Context, id uint64) (*domain.ScheduledTask, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	var row domain.ScheduledTask
	err := s.db.WithContext(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load scheduled task id=%d: %w", id, err)
	}
	return &row, nil
}

func normalizeInput(input Input, now time.Time, location *time.Location) (Input, time.Time, error) {
	input.DispatchKind = strings.TrimSpace(input.DispatchKind)
	input.Title = strings.TrimSpace(input.Title)
	input.ActionType = strings.TrimSpace(input.ActionType)
	input.Instruction = strings.TrimSpace(input.Instruction)
	input.ScheduleType = strings.TrimSpace(input.ScheduleType)
	if input.ActionType == "" {
		input.ActionType = "agent_task"
	}
	if input.DispatchKind == "" {
		input.DispatchKind = "create_task"
	}
	if input.initialStatus == "" {
		input.initialStatus = "active"
	}
	if input.initialStatus != "active" && input.initialStatus != "binding" {
		return Input{}, time.Time{}, fmt.Errorf("%w: initial scheduled task status is invalid", ErrInvalidInput)
	}
	switch input.DispatchKind {
	case "create_task":
		if input.SubjectType != nil || input.SubjectID != nil || input.SourceRunID != nil {
			return Input{}, time.Time{}, fmt.Errorf("%w: create_task cannot bind a continuation subject", ErrInvalidInput)
		}
	case "resume_task":
		if input.SubjectType == nil || strings.TrimSpace(*input.SubjectType) != "task" || input.SubjectID == nil || *input.SubjectID == 0 {
			return Input{}, time.Time{}, fmt.Errorf("%w: resume_task requires subject_type=task and subject_id", ErrInvalidInput)
		}
		if input.initialStatus != "binding" {
			return Input{}, time.Time{}, fmt.Errorf("%w: resume_task must be created through yield-until", ErrInvalidInput)
		}
		if input.ScheduleType != "once" {
			return Input{}, time.Time{}, fmt.Errorf("%w: resume_task must use a once schedule", ErrInvalidInput)
		}
	default:
		return Input{}, time.Time{}, fmt.Errorf("%w: dispatch_kind must be create_task or resume_task", ErrInvalidInput)
	}
	if input.Title == "" || input.Instruction == "" {
		return Input{}, time.Time{}, fmt.Errorf("%w: title and instruction are required", ErrInvalidInput)
	}
	if location == nil {
		return Input{}, time.Time{}, fmt.Errorf("scheduled task location is nil")
	}
	contextJSON, err := canonicalObject(input.ContextSnapshot)
	if err != nil {
		return Input{}, time.Time{}, fmt.Errorf("%w: context_snapshot: %v", ErrInvalidInput, err)
	}
	input.ContextSnapshot = contextJSON
	dispatchPayload, err := canonicalObject(input.DispatchPayload)
	if err != nil {
		return Input{}, time.Time{}, fmt.Errorf("%w: dispatch_payload: %v", ErrInvalidInput, err)
	}
	input.DispatchPayload = dispatchPayload
	if input.Enabled == nil {
		enabled := true
		input.Enabled = &enabled
	}
	switch input.ScheduleType {
	case "daily":
		if input.DailyTime == nil {
			return Input{}, time.Time{}, fmt.Errorf("%w: daily_time is required for daily schedule", ErrInvalidInput)
		}
		dailyTime := strings.TrimSpace(*input.DailyTime)
		if _, _, err := parseDailyTime(dailyTime); err != nil {
			return Input{}, time.Time{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		input.DailyTime = &dailyTime
		input.Weekday = nil
		input.IntervalMinutes = nil
		input.RunAt = nil
	case "weekly":
		if input.Weekday == nil || *input.Weekday < 1 || *input.Weekday > 7 {
			return Input{}, time.Time{}, fmt.Errorf("%w: weekday must be between 1 and 7 for weekly schedule", ErrInvalidInput)
		}
		if input.DailyTime == nil {
			return Input{}, time.Time{}, fmt.Errorf("%w: daily_time is required for weekly schedule", ErrInvalidInput)
		}
		dailyTime := strings.TrimSpace(*input.DailyTime)
		if _, _, err := parseDailyTime(dailyTime); err != nil {
			return Input{}, time.Time{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		input.DailyTime = &dailyTime
		input.IntervalMinutes = nil
		input.RunAt = nil
	case "interval":
		if input.IntervalMinutes == nil || *input.IntervalMinutes <= 0 {
			return Input{}, time.Time{}, fmt.Errorf("%w: interval_minutes must be positive for interval schedule", ErrInvalidInput)
		}
		input.DailyTime = nil
		input.Weekday = nil
		input.RunAt = nil
	case "once":
		if input.RunAt == nil || input.RunAt.IsZero() {
			return Input{}, time.Time{}, fmt.Errorf("%w: run_at is required for once schedule", ErrInvalidInput)
		}
		runAt := input.RunAt.UTC()
		input.RunAt = &runAt
		input.DailyTime = nil
		input.Weekday = nil
		input.IntervalMinutes = nil
	default:
		return Input{}, time.Time{}, fmt.Errorf("%w: schedule_type must be once, daily, weekly or interval", ErrInvalidInput)
	}
	nextRunAt, err := nextOccurrenceFromInput(input, now, location)
	if err != nil {
		return Input{}, time.Time{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return input, nextRunAt, nil
}

func nextOccurrenceFromInput(input Input, after time.Time, location *time.Location) (time.Time, error) {
	if (input.ScheduleType == "daily" || input.ScheduleType == "weekly") && input.DailyTime == nil {
		return time.Time{}, fmt.Errorf("daily_time is required")
	}
	switch input.ScheduleType {
	case "daily":
		hour, minute, err := parseDailyTime(*input.DailyTime)
		if err != nil {
			return time.Time{}, err
		}
		localAfter := after.In(location)
		candidate := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), hour, minute, 0, 0, location)
		if !candidate.After(localAfter) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate.UTC(), nil
	case "weekly":
		if input.Weekday == nil || *input.Weekday < 1 || *input.Weekday > 7 {
			return time.Time{}, fmt.Errorf("weekday must be between 1 and 7")
		}
		hour, minute, err := parseDailyTime(*input.DailyTime)
		if err != nil {
			return time.Time{}, err
		}
		localAfter := after.In(location)
		currentWeekday := int(localAfter.Weekday())
		if currentWeekday == 0 {
			currentWeekday = 7
		}
		daysUntil := (*input.Weekday - currentWeekday + 7) % 7
		candidateDate := localAfter.AddDate(0, 0, daysUntil)
		candidate := time.Date(candidateDate.Year(), candidateDate.Month(), candidateDate.Day(), hour, minute, 0, 0, location)
		if !candidate.After(localAfter) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		return candidate.UTC(), nil
	case "interval":
		return after.Add(time.Duration(*input.IntervalMinutes) * time.Minute).UTC(), nil
	case "once":
		if input.RunAt == nil || input.RunAt.IsZero() {
			return time.Time{}, fmt.Errorf("run_at is required for once schedule")
		}
		return input.RunAt.UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("unknown schedule_type %q", input.ScheduleType)
	}
}

func nextOccurrence(task *domain.ScheduledTask, after time.Time, location *time.Location) (time.Time, error) {
	if task == nil {
		return time.Time{}, fmt.Errorf("scheduled task is nil")
	}
	switch task.ScheduleType {
	case "daily":
		if task.DailyTime == nil {
			return time.Time{}, fmt.Errorf("daily_time is empty")
		}
		input := Input{ScheduleType: "daily", DailyTime: task.DailyTime}
		return nextOccurrenceFromInput(input, after, location)
	case "weekly":
		if task.DailyTime == nil {
			return time.Time{}, fmt.Errorf("daily_time is empty")
		}
		if task.Weekday == nil {
			return time.Time{}, fmt.Errorf("weekday is empty")
		}
		input := Input{ScheduleType: "weekly", DailyTime: task.DailyTime, Weekday: task.Weekday}
		return nextOccurrenceFromInput(input, after, location)
	case "interval":
		if task.IntervalMinutes == nil || *task.IntervalMinutes <= 0 {
			return time.Time{}, fmt.Errorf("interval_minutes must be positive")
		}
		interval := time.Duration(*task.IntervalMinutes) * time.Minute
		next := task.NextRunAt.UTC()
		if next.After(after) {
			return next, nil
		}
		steps := after.Sub(next)/interval + 1
		return next.Add(steps * interval), nil
	default:
		return time.Time{}, fmt.Errorf("unknown schedule_type %q", task.ScheduleType)
	}
}

func parseDailyTime(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return 0, 0, fmt.Errorf("daily_time must use HH:mm")
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func canonicalObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var object map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("must be a JSON object: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("must contain exactly one JSON object")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode trailing JSON: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("must be a JSON object")
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("encode JSON object: %w", err)
	}
	return encoded, nil
}

func toView(row *domain.ScheduledTask) View {
	return View{
		ID: row.ID, DispatchKind: row.DispatchKind, SubjectType: row.SubjectType,
		SubjectID: row.SubjectID, SourceRunID: row.SourceRunID,
		DispatchPayload: json.RawMessage(append([]byte(nil), row.DispatchPayload...)),
		Title:           row.Title, ActionType: row.ActionType, Instruction: row.Instruction,
		ContextSnapshot: json.RawMessage(append([]byte(nil), row.ContextSnapshot...)),
		ScheduleType:    row.ScheduleType, DailyTime: row.DailyTime, Weekday: row.Weekday,
		IntervalMinutes: row.IntervalMinutes, RunAt: row.RunAt, NextRunAt: row.NextRunAt,
		Enabled: row.Enabled, Status: row.Status, LastRunStatus: row.LastRunStatus, LastTaskID: row.LastTaskID,
		LastResult: row.LastResult, LastErrorDetail: row.LastErrorDetail,
		LastStartedAt: row.LastStartedAt, LastFinishedAt: row.LastFinishedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
