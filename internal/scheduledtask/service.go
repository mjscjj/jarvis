// Package scheduledtask implements one-shot Codex tasks backed by MySQL.
package scheduledtask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const executionSandbox = "danger-full-access"

var (
	ErrInvalidInput = errors.New("invalid scheduled task input")
	ErrNotFound     = errors.New("scheduled task not found")
	ErrRunning      = errors.New("scheduled task is running")
)

var validStatuses = map[string]struct{}{
	"pending": {}, "running": {}, "done": {}, "failed": {},
}

type Input struct {
	Title           string          `json:"title"`
	Instruction     string          `json:"instruction"`
	ContextSnapshot json.RawMessage `json:"context_snapshot"`
	ScheduledAt     time.Time       `json:"scheduled_at"`
}

type View struct {
	ID              uint64          `json:"id"`
	Title           string          `json:"title"`
	Instruction     string          `json:"instruction"`
	ContextSnapshot json.RawMessage `json:"context_snapshot"`
	ScheduledAt     time.Time       `json:"scheduled_at"`
	Status          string          `json:"status"`
	Result          *string         `json:"result"`
	ErrorDetail     *string         `json:"error_detail"`
	StartedAt       *time.Time      `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type ListFilter struct {
	Status string
	Limit  int
}

// Runner is the free-text Codex surface shared with chat-like callers.
type Runner interface {
	RunTextSandbox(context.Context, string, string) (string, error)
}

type Service struct {
	db          *gorm.DB
	runner      Runner
	concurrency int
	batchLimit  int
	slots       chan struct{}
	now         func() time.Time
}

// NewCRUDService constructs the storage-only surface used by jarvis-tools.
// Execution methods still fail fast if called without the full NewService.
func NewCRUDService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("scheduled task db is nil")
	}
	return &Service{db: db, now: time.Now}, nil
}

func NewService(db *gorm.DB, runner Runner, concurrency, batchLimit int) (*Service, error) {
	service, err := NewCRUDService(db)
	if err != nil {
		return nil, err
	}
	if runner == nil {
		return nil, fmt.Errorf("scheduled task runner is nil")
	}
	if concurrency <= 0 {
		return nil, fmt.Errorf("scheduled task concurrency must be positive")
	}
	if batchLimit <= 0 {
		return nil, fmt.Errorf("scheduled task batch limit must be positive")
	}
	service.runner = runner
	service.concurrency = concurrency
	service.batchLimit = batchLimit
	service.slots = make(chan struct{}, concurrency)
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
	if err := query.Order("scheduled_at DESC, id DESC").Limit(filter.Limit).Find(&rows).Error; err != nil {
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
	normalized, err := normalizeInput(input)
	if err != nil {
		return nil, err
	}
	row := domain.ScheduledTask{
		Title: normalized.Title, Instruction: normalized.Instruction,
		ContextSnapshot: datatypes.JSON(normalized.ContextSnapshot),
		ScheduledAt:     normalized.ScheduledAt.UTC(), Status: "pending",
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
	normalized, err := normalizeInput(input)
	if err != nil {
		return nil, err
	}
	result := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status <> ?", id, "running").
		Updates(map[string]any{
			"title": normalized.Title, "instruction": normalized.Instruction,
			"context_snapshot": datatypes.JSON(normalized.ContextSnapshot),
			"scheduled_at":     normalized.ScheduledAt.UTC(), "status": "pending",
			"result": nil, "error_detail": nil, "started_at": nil, "finished_at": nil,
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
		return s.Get(ctx, id)
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

// Trigger claims any non-running task and executes it asynchronously. It is the
// explicit override for future pending tasks and the rerun path for done/failed.
func (s *Service) Trigger(ctx context.Context, id uint64) (*View, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	now := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status <> ?", id, "running").
		Updates(map[string]any{
			"status": "running", "result": nil, "error_detail": nil,
			"started_at": now, "finished_at": nil,
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
	view, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	go s.execute(context.Background(), id)
	return view, nil
}

// RecoverRunning is called once at process startup. A previous local process is
// gone at this point, so its in-memory Codex executions cannot still complete.
func (s *Service) RecoverRunning(ctx context.Context) (int64, error) {
	result := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).Where("status = ?", "running").Updates(map[string]any{
		"status": "pending", "started_at": nil,
		"error_detail": "recovered after Jarvis process restart",
	})
	if result.Error != nil {
		return 0, fmt.Errorf("recover running scheduled tasks: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// RunDue claims all due tasks in one bounded batch and executes them with a
// fixed worker pool. The scheduler waits for the batch, so SkipIfStillRunning
// can suppress overlapping cron rounds.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	if s.runner == nil || s.concurrency <= 0 || s.batchLimit <= 0 {
		return 0, fmt.Errorf("scheduled task execution service is not configured")
	}
	claimed, err := s.claimDue(ctx, s.now().UTC())
	if err != nil || len(claimed) == 0 {
		return len(claimed), err
	}
	jobs := make(chan uint64)
	var wg sync.WaitGroup
	workerCount := s.concurrency
	if workerCount > len(claimed) {
		workerCount = len(claimed)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				s.execute(ctx, id)
			}
		}()
	}
	for _, row := range claimed {
		jobs <- row.ID
	}
	close(jobs)
	wg.Wait()
	return len(claimed), nil
}

func (s *Service) claimDue(ctx context.Context, now time.Time) ([]domain.ScheduledTask, error) {
	var candidates []domain.ScheduledTask
	if err := s.db.WithContext(ctx).Where("status = ? AND scheduled_at <= ?", "pending", now).
		Order("scheduled_at ASC, id ASC").Limit(s.batchLimit).Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("load due scheduled tasks: %w", err)
	}
	claimed := make([]domain.ScheduledTask, 0, len(candidates))
	for i := range candidates {
		startedAt := s.now().UTC()
		result := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
			Where("id = ? AND status = ?", candidates[i].ID, "pending").
			Updates(map[string]any{
				"status": "running", "result": nil, "error_detail": nil,
				"started_at": startedAt, "finished_at": nil,
			})
		if result.Error != nil {
			return claimed, fmt.Errorf("claim due scheduled task id=%d: %w", candidates[i].ID, result.Error)
		}
		if result.RowsAffected == 1 {
			claimed = append(claimed, candidates[i])
		}
	}
	return claimed, nil
}

func (s *Service) execute(ctx context.Context, id uint64) {
	if s.slots == nil {
		s.fail(ctx, id, fmt.Errorf("scheduled task execution slots are not configured"))
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		s.fail(context.Background(), id, ctx.Err())
		return
	}
	row, err := s.load(ctx, id)
	if err != nil {
		s.fail(ctx, id, err)
		return
	}
	prompt, err := buildPrompt(row)
	if err != nil {
		s.fail(ctx, id, err)
		return
	}
	result, err := s.runner.RunTextSandbox(ctx, prompt, executionSandbox)
	if err != nil {
		s.fail(ctx, id, err)
		return
	}
	result = strings.TrimSpace(result)
	if result == "" {
		s.fail(ctx, id, fmt.Errorf("Codex returned empty result"))
		return
	}
	finishedAt := s.now().UTC()
	update := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]any{"status": "done", "result": result, "error_detail": nil, "finished_at": finishedAt})
	if update.Error != nil {
		s.fail(ctx, id, fmt.Errorf("store scheduled task result: %w", update.Error))
	}
}

func (s *Service) fail(ctx context.Context, id uint64, cause error) {
	if cause == nil {
		return
	}
	detail := cause.Error()
	finishedAt := s.now().UTC()
	if err := s.db.WithContext(ctx).Model(&domain.ScheduledTask{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]any{"status": "failed", "error_detail": detail, "finished_at": finishedAt}).Error; err != nil {
		log.Printf("scheduled task id=%d store failure status error=%v original_error=%v", id, err, cause)
	}
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

func normalizeInput(input Input) (Input, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if input.Title == "" || input.Instruction == "" || input.ScheduledAt.IsZero() {
		return Input{}, fmt.Errorf("%w: title, instruction and scheduled_at are required", ErrInvalidInput)
	}
	contextJSON, err := canonicalObject(input.ContextSnapshot)
	if err != nil {
		return Input{}, fmt.Errorf("%w: context_snapshot: %v", ErrInvalidInput, err)
	}
	input.ContextSnapshot = contextJSON
	return input, nil
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
		ID: row.ID, Title: row.Title, Instruction: row.Instruction,
		ContextSnapshot: json.RawMessage(append([]byte(nil), row.ContextSnapshot...)),
		ScheduledAt:     row.ScheduledAt, Status: row.Status,
		Result: row.Result, ErrorDetail: row.ErrorDetail,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
