package insight

import (
	"context"
	"errors"
	"fmt"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var ErrProactiveRunNotFound = errors.New("proactive run not found")

type ProactiveRunRow struct {
	ID          uint64  `json:"id"`
	TriggerType string  `json:"trigger_type"`
	Engine      string  `json:"engine"`
	Model       string  `json:"model"`
	Status      string  `json:"status"`
	ErrorDetail *string `json:"error_detail"`
	StartedAt   string  `json:"started_at"`
	FinishedAt  *string `json:"finished_at"`
	DurationMS  *int64  `json:"duration_ms"`
}

type ProactiveRunDetail struct {
	ProactiveRunRow
	Input  string  `json:"input"`
	Output *string `json:"output"`
}

func (s *DebugService) ProactiveRuns(ctx context.Context, limit int) ([]ProactiveRunRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var records []domain.ProactiveRun
	if err := s.db.WithContext(ctx).
		Select("id", "trigger_type", "engine", "model", "status", "error_detail", "started_at", "finished_at", "duration_ms").
		Order("id DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("load proactive runs: %w", err)
	}
	rows := make([]ProactiveRunRow, len(records))
	for i := range records {
		rows[i] = proactiveRunRow(records[i])
	}
	return rows, nil
}

func (s *DebugService) ProactiveRun(ctx context.Context, id uint64) (*ProactiveRunDetail, error) {
	if id == 0 {
		return nil, fmt.Errorf("proactive run id is required")
	}
	var record domain.ProactiveRun
	if err := s.db.WithContext(ctx).First(&record, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: id=%d", ErrProactiveRunNotFound, id)
		}
		return nil, fmt.Errorf("load proactive run id=%d: %w", id, err)
	}
	return &ProactiveRunDetail{
		ProactiveRunRow: proactiveRunRow(record),
		Input:           record.Input,
		Output:          record.Output,
	}, nil
}

func proactiveRunRow(record domain.ProactiveRun) ProactiveRunRow {
	var finishedAt *string
	if record.FinishedAt != nil {
		value := record.FinishedAt.Format(time.RFC3339)
		finishedAt = &value
	}
	return ProactiveRunRow{
		ID: record.ID, TriggerType: record.TriggerType, Engine: record.Engine,
		Model: record.Model, Status: record.Status, ErrorDetail: record.ErrorDetail,
		StartedAt: record.StartedAt.Format(time.RFC3339), FinishedAt: finishedAt,
		DurationMS: record.DurationMS,
	}
}
