package proactive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"

	RunStatusRunning   = "running"
	RunStatusSucceeded = "succeeded"
	RunStatusFailed    = "failed"
)

type Recorder interface {
	Start(context.Context, string, string, string, string, time.Time) (uint64, error)
	Succeed(context.Context, uint64, string, time.Time) error
	Fail(context.Context, uint64, string, time.Time) error
}

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("proactive run store db is nil")
	}
	return &Store{db: db}, nil
}

func (s *Store) Start(ctx context.Context, trigger, engine, model, input string, startedAt time.Time) (uint64, error) {
	trigger = strings.TrimSpace(trigger)
	engine = strings.TrimSpace(engine)
	model = strings.TrimSpace(model)
	input = strings.TrimSpace(input)
	if trigger != TriggerSchedule && trigger != TriggerManual {
		return 0, fmt.Errorf("proactive run trigger must be schedule or manual")
	}
	if engine == "" || model == "" || input == "" || startedAt.IsZero() {
		return 0, fmt.Errorf("proactive run engine, model, input and started_at are required")
	}
	record := domain.ProactiveRun{
		TriggerType: trigger,
		Engine:      engine,
		Model:       model,
		Status:      RunStatusRunning,
		Input:       input,
		StartedAt:   startedAt,
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return 0, fmt.Errorf("create proactive run: %w", err)
	}
	return record.ID, nil
}

func (s *Store) Succeed(ctx context.Context, id uint64, output string, finishedAt time.Time) error {
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Errorf("proactive run success output is required")
	}
	return s.finish(ctx, id, RunStatusSucceeded, &output, nil, finishedAt)
}

func (s *Store) Fail(ctx context.Context, id uint64, errorDetail string, finishedAt time.Time) error {
	errorDetail = strings.TrimSpace(errorDetail)
	if errorDetail == "" {
		return fmt.Errorf("proactive run failure detail is required")
	}
	return s.finish(ctx, id, RunStatusFailed, nil, &errorDetail, finishedAt)
}

func (s *Store) finish(ctx context.Context, id uint64, status string, output, errorDetail *string, finishedAt time.Time) error {
	if id == 0 || finishedAt.IsZero() {
		return fmt.Errorf("proactive run id and finished_at are required")
	}
	var record domain.ProactiveRun
	if err := s.db.WithContext(ctx).Select("id", "status", "started_at").First(&record, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("proactive run id=%d not found: %w", id, err)
		}
		return fmt.Errorf("load proactive run id=%d: %w", id, err)
	}
	if record.Status != RunStatusRunning {
		return fmt.Errorf("proactive run id=%d status=%s is not running", id, record.Status)
	}
	duration := finishedAt.Sub(record.StartedAt).Milliseconds()
	if duration < 0 {
		return fmt.Errorf("proactive run id=%d finished before it started", id)
	}
	updates := map[string]any{
		"status": status, "output": output, "error_detail": errorDetail,
		"finished_at": finishedAt, "duration_ms": duration,
	}
	result := s.db.WithContext(ctx).Model(&domain.ProactiveRun{}).
		Where("id = ? AND status = ?", id, RunStatusRunning).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("finish proactive run id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("finish proactive run id=%d: expected one running row, updated %d", id, result.RowsAffected)
	}
	return nil
}
