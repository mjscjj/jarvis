package insight

import (
	"context"
	"fmt"
	"time"

	"jarvis/internal/domain"
)

type MonitoringSnapshot struct {
	From  string            `json:"from"`
	Until string            `json:"until"`
	M3    ExtractionMetrics `json:"m3"`
	M5    ExecutionMetrics  `json:"m5"`
}

type ExtractionMetrics struct {
	ProcessedMessages     int64    `json:"processed_messages"`
	TodosCreated          int64    `json:"todos_created"`
	AverageDurationMS     *float64 `json:"average_duration_ms"`
	MaxDurationMS         *int64   `json:"max_duration_ms"`
	TotalTokens           *int64   `json:"total_tokens"`
	TokenCoverageComplete bool     `json:"token_coverage_complete"`
	FailedRuns            int64    `json:"failed_runs"`
}

type ExecutionMetrics struct {
	ProcessedTasks        int64    `json:"processed_tasks"`
	AverageDurationMS     *float64 `json:"average_duration_ms"`
	MaxDurationMS         *int64   `json:"max_duration_ms"`
	TotalTokens           *int64   `json:"total_tokens"`
	TokenCoverageComplete bool     `json:"token_coverage_complete"`
	FailedRuns            int64    `json:"failed_runs"`
}

type monitoringAggregate struct {
	ProcessedCount    int64    `gorm:"column:processed_count"`
	OutputCount       int64    `gorm:"column:output_count"`
	AverageDurationMS *float64 `gorm:"column:average_duration_ms"`
	MaxDurationMS     *int64   `gorm:"column:max_duration_ms"`
	TotalTokens       *int64   `gorm:"column:total_tokens"`
	FinishedRuns      int64    `gorm:"column:finished_runs"`
	TokenReportedRuns int64    `gorm:"column:token_reported_runs"`
	FailedRuns        int64    `gorm:"column:failed_runs"`
}

// Monitoring aggregates persisted M3/M5 run records. It intentionally does
// not parse process logs: dashboards must stay cheap and stable as logs rotate.
func (s *DebugService) Monitoring(ctx context.Context, from, until time.Time) (*MonitoringSnapshot, error) {
	if from.IsZero() || until.IsZero() {
		return nil, fmt.Errorf("monitoring range requires from and until")
	}
	from, until = from.UTC(), until.UTC()
	if !from.Before(until) {
		return nil, fmt.Errorf("monitoring from must be before until")
	}

	var m3 monitoringAggregate
	if err := s.db.WithContext(ctx).Model(&domain.ExtractionRun{}).
		Where("started_at >= ? AND started_at < ?", from, until).
		Select(`
			COALESCE(SUM(CASE WHEN status = 'succeeded' THEN message_count ELSE 0 END), 0) AS processed_count,
			COALESCE(SUM(CASE WHEN status = 'succeeded' THEN todo_count ELSE 0 END), 0) AS output_count,
			AVG(CASE WHEN finished_at IS NOT NULL THEN duration_ms END) AS average_duration_ms,
			MAX(CASE WHEN finished_at IS NOT NULL THEN duration_ms END) AS max_duration_ms,
			SUM(CASE WHEN input_tokens IS NOT NULL AND output_tokens IS NOT NULL THEN input_tokens + output_tokens END) AS total_tokens,
			COALESCE(SUM(CASE WHEN finished_at IS NOT NULL THEN 1 ELSE 0 END), 0) AS finished_runs,
			COALESCE(SUM(CASE WHEN finished_at IS NOT NULL AND input_tokens IS NOT NULL AND output_tokens IS NOT NULL THEN 1 ELSE 0 END), 0) AS token_reported_runs,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS failed_runs`).
		Scan(&m3).Error; err != nil {
		return nil, fmt.Errorf("aggregate M3 monitoring: %w", err)
	}

	var m5 monitoringAggregate
	if err := s.db.WithContext(ctx).Model(&domain.ExecutionRun{}).
		Where("started_at >= ? AND started_at < ?", from, until).
		Select(`
			COUNT(DISTINCT CASE WHEN finished_at IS NOT NULL THEN task_id END) AS processed_count,
			AVG(CASE WHEN finished_at IS NOT NULL THEN duration_ms END) AS average_duration_ms,
			MAX(CASE WHEN finished_at IS NOT NULL THEN duration_ms END) AS max_duration_ms,
			SUM(CASE WHEN input_tokens IS NOT NULL AND output_tokens IS NOT NULL THEN input_tokens + output_tokens END) AS total_tokens,
			COALESCE(SUM(CASE WHEN finished_at IS NOT NULL THEN 1 ELSE 0 END), 0) AS finished_runs,
			COALESCE(SUM(CASE WHEN finished_at IS NOT NULL AND input_tokens IS NOT NULL AND output_tokens IS NOT NULL THEN 1 ELSE 0 END), 0) AS token_reported_runs,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS failed_runs`).
		Scan(&m5).Error; err != nil {
		return nil, fmt.Errorf("aggregate M5 monitoring: %w", err)
	}

	return &MonitoringSnapshot{
		From: from.Format(time.RFC3339Nano), Until: until.Format(time.RFC3339Nano),
		M3: ExtractionMetrics{
			ProcessedMessages: m3.ProcessedCount, TodosCreated: m3.OutputCount,
			AverageDurationMS: m3.AverageDurationMS, MaxDurationMS: m3.MaxDurationMS,
			TotalTokens: m3.TotalTokens, TokenCoverageComplete: completeTokenCoverage(m3),
			FailedRuns: m3.FailedRuns,
		},
		M5: ExecutionMetrics{
			ProcessedTasks:    m5.ProcessedCount,
			AverageDurationMS: m5.AverageDurationMS, MaxDurationMS: m5.MaxDurationMS,
			TotalTokens: m5.TotalTokens, TokenCoverageComplete: completeTokenCoverage(m5),
			FailedRuns: m5.FailedRuns,
		},
	}, nil
}

func completeTokenCoverage(aggregate monitoringAggregate) bool {
	return aggregate.FinishedRuns > 0 && aggregate.TokenReportedRuns == aggregate.FinishedRuns
}
