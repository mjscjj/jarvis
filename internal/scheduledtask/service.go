// Package scheduledtask implements durable time triggers backed by MySQL.
package scheduledtask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrInvalidInput = errors.New("invalid scheduled task input")
	ErrNotFound     = errors.New("scheduled task not found")
	ErrRunning      = errors.New("scheduled task is running")
)

var validStatuses = map[string]struct{}{"active": {}, "running": {}, "completed": {}}

type Input struct {
	Title           string          `json:"title"`
	ActionType      string          `json:"action_type"`
	Instruction     string          `json:"instruction"`
	ContextSnapshot json.RawMessage `json:"context_snapshot"`
	ScheduleType    string          `json:"schedule_type"`
	DailyTime       *string         `json:"daily_time"`
	IntervalMinutes *int            `json:"interval_minutes"`
	RunAt           *time.Time      `json:"run_at"`
	Enabled         *bool           `json:"enabled"`
}

type View struct {
	ID              uint64          `json:"id"`
	Title           string          `json:"title"`
	ActionType      string          `json:"action_type"`
	Instruction     string          `json:"instruction"`
	ContextSnapshot json.RawMessage `json:"context_snapshot"`
	ScheduleType    string          `json:"schedule_type"`
	DailyTime       *string         `json:"daily_time"`
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
	Status string
	Limit  int
}

type TaskSubmitter interface {
	Submit(context.Context, taskcreate.Input) (*domain.Task, error)
}

type Service struct {
	db         *gorm.DB
	submitter  TaskSubmitter
	batchLimit int
	now        func() time.Time
	location   *time.Location
}

// NewCRUDService constructs the storage-only surface used by jarvis-tools.
// Execution methods still fail fast if called without the full NewService.
func NewCRUDService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("scheduled task db is nil")
	}
	return &Service{db: db, now: time.Now, location: time.Local}, nil
}

func NewService(db *gorm.DB, submitter TaskSubmitter, batchLimit int) (*Service, error) {
	service, err := NewCRUDService(db)
	if err != nil {
		return nil, err
	}
	if submitter == nil {
		return nil, fmt.Errorf("scheduled task Task submitter is nil")
	}
	if batchLimit <= 0 {
		return nil, fmt.Errorf("scheduled task batch limit must be positive")
	}
	service.submitter = submitter
	service.batchLimit = batchLimit
	return service, nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]View, error) {
	if filter.Limit <= 0 || filter.Limit > 500 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 500", ErrInvalidInput)
	}
	query := s.db.WithContext(ctx).Model(&domain.ScheduledTask{})
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
		Title: normalized.Title, ActionType: normalized.ActionType, Instruction: normalized.Instruction,
		ContextSnapshot: datatypes.JSON(normalized.ContextSnapshot),
		ScheduleType:    normalized.ScheduleType, DailyTime: normalized.DailyTime,
		IntervalMinutes: normalized.IntervalMinutes, RunAt: normalized.RunAt, NextRunAt: nextRunAt,
		Enabled: *normalized.Enabled, Status: "active",
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, fmt.Errorf("create scheduled task: %w", err)
	}
	return s.Get(ctx, row.ID)
}

func (s *Service) Update(ctx context.Context, id uint64, input Input) (*View, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
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
			"schedule_type":    normalized.ScheduleType, "daily_time": normalized.DailyTime,
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
	result := s.db.WithContext(ctx).Where("id = ? AND status <> ?", id, "running").Delete(&domain.ScheduledTask{})
	if result.Error != nil {
		return fmt.Errorf("delete scheduled task id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 0 {
		return nil
	}
	row, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	if row.Status == "running" {
		return ErrRunning
	}
	return fmt.Errorf("delete scheduled task id=%d affected no rows", id)
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
	if err := s.dispatch(context.Background(), row, occurrenceKey); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// RecoverRunning is called once at process startup. A schedule left in running
// did not finish materializing its Task and must become claimable again.
func (s *Service) RecoverRunning(ctx context.Context) (int64, error) {
	now := s.now().UTC()
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
	return recurring.RowsAffected + oneTime.RowsAffected, nil
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
	if row == nil || row.ID == 0 {
		return fmt.Errorf("scheduled task dispatch row is invalid")
	}
	input, err := taskInput(row, occurrenceKey)
	if err != nil {
		s.fail(context.Background(), row.ID, err)
		return err
	}
	task, err := s.submitter.Submit(ctx, input)
	if err != nil {
		if task != nil {
			_ = s.db.WithContext(context.Background()).Model(&domain.ScheduledTask{}).
				Where("id = ?", row.ID).Update("last_task_id", task.ID).Error
		}
		s.fail(context.Background(), row.ID, err)
		return err
	}
	finishedAt := s.now().UTC()
	finalStatus := finalTaskStatus(row.ScheduleType)
	result := fmt.Sprintf("已创建并提交 Task #%d；实际执行结果以该 Task 为准", task.ID)
	update := s.db.WithContext(context.Background()).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status = ?", row.ID, "running").
		Updates(map[string]any{
			"status": finalStatus, "last_run_status": "done", "last_result": result,
			"last_task_id": task.ID, "last_error_detail": nil, "last_finished_at": finishedAt,
		})
	if update.Error != nil {
		s.fail(context.Background(), row.ID, fmt.Errorf("store scheduled task trigger result: %w", update.Error))
		return update.Error
	}
	return nil
}

func taskInput(row *domain.ScheduledTask, occurrenceKey string) (taskcreate.Input, error) {
	if row == nil || row.ID == 0 {
		return taskcreate.Input{}, fmt.Errorf("scheduled task row is invalid")
	}
	occurrenceKey = strings.TrimSpace(occurrenceKey)
	if occurrenceKey == "" {
		return taskcreate.Input{}, fmt.Errorf("scheduled task occurrence key is empty")
	}
	plan, err := json.Marshal(map[string]any{"instruction": row.Instruction})
	if err != nil {
		return taskcreate.Input{}, fmt.Errorf("encode scheduled Task plan: %w", err)
	}
	approvalRef := fmt.Sprintf("scheduled_task:%d", row.ID)
	return taskcreate.Input{
		Title: row.Title, ActionType: row.ActionType, Target: row.Title,
		Background: json.RawMessage(row.ContextSnapshot), Plan: plan,
		ConfirmedBy: "scheduled_task", SourceType: taskcreate.SourceScheduledTask,
		SourceID: &row.ID, OccurrenceKey: &occurrenceKey,
		ExecutionMode: taskcreate.ExecutionModeDirect, ApprovalRef: &approvalRef,
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
		log.Printf("scheduled task id=%d load before failure update error=%v original_error=%v", id, loadErr, cause)
		return
	}
	if err := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]any{
			"status": finalTaskStatus(row.ScheduleType), "last_run_status": "failed",
			"last_error_detail": cause.Error(), "last_finished_at": finishedAt,
		}).Error; err != nil {
		log.Printf("scheduled task id=%d store failure status error=%v original_error=%v", id, err, cause)
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
	input.Title = strings.TrimSpace(input.Title)
	input.ActionType = strings.TrimSpace(input.ActionType)
	input.Instruction = strings.TrimSpace(input.Instruction)
	input.ScheduleType = strings.TrimSpace(input.ScheduleType)
	if input.ActionType == "" {
		input.ActionType = "agent_task"
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
		input.IntervalMinutes = nil
		input.RunAt = nil
	case "interval":
		if input.IntervalMinutes == nil || *input.IntervalMinutes <= 0 {
			return Input{}, time.Time{}, fmt.Errorf("%w: interval_minutes must be positive for interval schedule", ErrInvalidInput)
		}
		input.DailyTime = nil
		input.RunAt = nil
	case "once":
		if input.RunAt == nil || input.RunAt.IsZero() {
			return Input{}, time.Time{}, fmt.Errorf("%w: run_at is required for once schedule", ErrInvalidInput)
		}
		runAt := input.RunAt.UTC()
		input.RunAt = &runAt
		input.DailyTime = nil
		input.IntervalMinutes = nil
	default:
		return Input{}, time.Time{}, fmt.Errorf("%w: schedule_type must be once, daily or interval", ErrInvalidInput)
	}
	nextRunAt, err := nextOccurrenceFromInput(input, now, location)
	if err != nil {
		return Input{}, time.Time{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return input, nextRunAt, nil
}

func nextOccurrenceFromInput(input Input, after time.Time, location *time.Location) (time.Time, error) {
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
		ID: row.ID, Title: row.Title, ActionType: row.ActionType, Instruction: row.Instruction,
		ContextSnapshot: json.RawMessage(append([]byte(nil), row.ContextSnapshot...)),
		ScheduleType:    row.ScheduleType, DailyTime: row.DailyTime,
		IntervalMinutes: row.IntervalMinutes, RunAt: row.RunAt, NextRunAt: row.NextRunAt,
		Enabled: row.Enabled, Status: row.Status, LastRunStatus: row.LastRunStatus, LastTaskID: row.LastTaskID,
		LastResult: row.LastResult, LastErrorDetail: row.LastErrorDetail,
		LastStartedAt: row.LastStartedAt, LastFinishedAt: row.LastFinishedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
