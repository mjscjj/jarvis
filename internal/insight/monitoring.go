package insight

import (
	"context"
	"fmt"
	"time"

	"jarvis/internal/domain"
)

type MonitoringSnapshot struct {
	From   string            `json:"from"`
	Until  string            `json:"until"`
	Bucket string            `json:"bucket"`
	M2     CaptureMetrics    `json:"m2"`
	M3     ExtractionMetrics `json:"m3"`
	M5     ExecutionMetrics  `json:"m5"`
}

type MonitoringPoint struct {
	BucketStart           string   `json:"bucket_start"`
	RecordingActive       bool     `json:"recording_active"`
	ScopeCount            int64    `json:"scope_count"`
	RunCount              int64    `json:"run_count"`
	AverageDurationMS     *float64 `json:"average_duration_ms"`
	FinishedRuns          int64    `json:"finished_runs"`
	FailedRuns            int64    `json:"failed_runs"`
	FailureRate           *float64 `json:"failure_rate"`
	TotalTokens           *int64   `json:"total_tokens"`
	TokenCoverageComplete bool     `json:"token_coverage_complete"`
}

type ExtractionMetrics struct {
	ChatCount             int64             `json:"chat_count"`
	RunCount              int64             `json:"run_count"`
	ProcessedMessages     int64             `json:"processed_messages"`
	TodosCreated          int64             `json:"todos_created"`
	AverageDurationMS     *float64          `json:"average_duration_ms"`
	MaxDurationMS         *int64            `json:"max_duration_ms"`
	TotalTokens           *int64            `json:"total_tokens"`
	TokenCoverageComplete bool              `json:"token_coverage_complete"`
	FinishedRuns          int64             `json:"finished_runs"`
	FailedRuns            int64             `json:"failed_runs"`
	FailureRate           *float64          `json:"failure_rate"`
	RecordedSince         *string           `json:"recorded_since"`
	Series                []MonitoringPoint `json:"series"`
}

type CaptureMetrics struct {
	InsertedMessages  int64             `json:"inserted_messages"`
	RunCount          int64             `json:"run_count"`
	AverageDurationMS *float64          `json:"average_duration_ms"`
	MaxDurationMS     *int64            `json:"max_duration_ms"`
	FinishedRuns      int64             `json:"finished_runs"`
	FailedRuns        int64             `json:"failed_runs"`
	FailureRate       *float64          `json:"failure_rate"`
	RecordedSince     *string           `json:"recorded_since"`
	Series            []MonitoringPoint `json:"series"`
}

type ExecutionMetrics struct {
	ProcessedTasks        int64             `json:"processed_tasks"`
	RunCount              int64             `json:"run_count"`
	AverageDurationMS     *float64          `json:"average_duration_ms"`
	MaxDurationMS         *int64            `json:"max_duration_ms"`
	TotalTokens           *int64            `json:"total_tokens"`
	TokenCoverageComplete bool              `json:"token_coverage_complete"`
	FinishedRuns          int64             `json:"finished_runs"`
	FailedRuns            int64             `json:"failed_runs"`
	FailureRate           *float64          `json:"failure_rate"`
	RecordedSince         *string           `json:"recorded_since"`
	Series                []MonitoringPoint `json:"series"`
}

type extractionStartRow struct {
	ChatID       string    `gorm:"column:chat_id"`
	Status       string    `gorm:"column:status"`
	MessageCount int64     `gorm:"column:message_count"`
	TodoCount    int64     `gorm:"column:todo_count"`
	StartedAt    time.Time `gorm:"column:started_at"`
}

type executionStartRow struct {
	TaskID    uint64    `gorm:"column:task_id"`
	StartedAt time.Time `gorm:"column:started_at"`
}

type completionRow struct {
	Status       string     `gorm:"column:status"`
	FinishedAt   *time.Time `gorm:"column:finished_at"`
	DurationMS   *int64     `gorm:"column:duration_ms"`
	InputTokens  *int64     `gorm:"column:input_tokens"`
	OutputTokens *int64     `gorm:"column:output_tokens"`
}

type captureCompletionRow struct {
	Status        string     `gorm:"column:status"`
	FinishedAt    *time.Time `gorm:"column:finished_at"`
	DurationMS    *int64     `gorm:"column:duration_ms"`
	InsertedCount int64      `gorm:"column:inserted_count"`
}

type pointAccumulator struct {
	point             MonitoringPoint
	scopes            map[string]struct{}
	durationTotal     int64
	durationRuns      int64
	tokenTotal        int64
	tokenReportedRuns int64
}

// Monitoring reads the durable M2/M3/M5 run records. Starts own the run trend;
// completed outcomes own duration, failures and token usage, so a long run that
// crosses a bucket boundary is attributed to when its result actually appeared.
func (s *DebugService) Monitoring(ctx context.Context, from, until time.Time) (*MonitoringSnapshot, error) {
	if from.IsZero() || until.IsZero() {
		return nil, fmt.Errorf("monitoring range requires from and until")
	}
	from, until = from.UTC(), until.UTC()
	if !from.Before(until) {
		return nil, fmt.Errorf("monitoring from must be before until")
	}
	bucket := time.Hour
	if until.Sub(from) > 24*time.Hour {
		bucket = 24 * time.Hour
	}

	var m2Starts []struct {
		StartedAt time.Time `gorm:"column:started_at"`
	}
	if err := s.db.WithContext(ctx).Model(&domain.ScanRecord{}).
		Select("started_at").
		Where("julianday(started_at) >= julianday(?) AND julianday(started_at) < julianday(?)", from, until).
		Order("started_at ASC").Scan(&m2Starts).Error; err != nil {
		return nil, fmt.Errorf("load M2 monitoring starts: %w", err)
	}
	var m2Completions []captureCompletionRow
	if err := s.db.WithContext(ctx).Model(&domain.ScanRecord{}).
		Select("status", "finished_at", "duration_ms", "inserted_count").
		Where("julianday(finished_at) >= julianday(?) AND julianday(finished_at) < julianday(?)", from, until).
		Order("finished_at ASC").Scan(&m2Completions).Error; err != nil {
		return nil, fmt.Errorf("load M2 monitoring completions: %w", err)
	}
	m2Recorded, err := s.monitoringRecordedSince(ctx, &domain.ScanRecord{})
	if err != nil {
		return nil, fmt.Errorf("load M2 monitoring boundary: %w", err)
	}

	var m3Starts []extractionStartRow
	if err := s.db.WithContext(ctx).Model(&domain.ExtractionRun{}).
		Select("chat_id", "status", "message_count", "todo_count", "started_at").
		Where("julianday(started_at) >= julianday(?) AND julianday(started_at) < julianday(?)", from, until).
		Order("started_at ASC").Scan(&m3Starts).Error; err != nil {
		return nil, fmt.Errorf("load M3 monitoring starts: %w", err)
	}
	var m3Completions []completionRow
	if err := s.db.WithContext(ctx).Model(&domain.ExtractionRun{}).
		Select("status", "finished_at", "duration_ms", "input_tokens", "output_tokens").
		Where("julianday(finished_at) >= julianday(?) AND julianday(finished_at) < julianday(?)", from, until).
		Order("finished_at ASC").Scan(&m3Completions).Error; err != nil {
		return nil, fmt.Errorf("load M3 monitoring completions: %w", err)
	}
	m3Recorded, err := s.monitoringRecordedSince(ctx, &domain.ExtractionRun{})
	if err != nil {
		return nil, fmt.Errorf("load M3 monitoring boundary: %w", err)
	}

	var m5Starts []executionStartRow
	if err := s.db.WithContext(ctx).Model(&domain.ExecutionRun{}).
		Select("task_id", "started_at").
		Where("julianday(started_at) >= julianday(?) AND julianday(started_at) < julianday(?)", from, until).
		Order("started_at ASC").Scan(&m5Starts).Error; err != nil {
		return nil, fmt.Errorf("load M5 monitoring starts: %w", err)
	}
	var m5Completions []completionRow
	if err := s.db.WithContext(ctx).Model(&domain.ExecutionRun{}).
		Select("status", "finished_at", "duration_ms", "input_tokens", "output_tokens").
		Where("julianday(finished_at) >= julianday(?) AND julianday(finished_at) < julianday(?)", from, until).
		Order("finished_at ASC").Scan(&m5Completions).Error; err != nil {
		return nil, fmt.Errorf("load M5 monitoring completions: %w", err)
	}
	m5Recorded, err := s.monitoringRecordedSince(ctx, &domain.ExecutionRun{})
	if err != nil {
		return nil, fmt.Errorf("load M5 monitoring boundary: %w", err)
	}

	m2Points := newMonitoringPoints(from, until, bucket, m2Recorded)
	for _, row := range m2Starts {
		monitoringPointAt(m2Points, from, bucket, row.StartedAt).point.RunCount++
	}
	var m2InsertedMessages int64
	m2CompletionRows := make([]completionRow, 0, len(m2Completions))
	for _, row := range m2Completions {
		point := monitoringPointAt(m2Points, from, bucket, *row.FinishedAt)
		point.point.ScopeCount += row.InsertedCount
		m2InsertedMessages += row.InsertedCount
		completion := completionRow{Status: row.Status, FinishedAt: row.FinishedAt, DurationMS: row.DurationMS}
		addCompletion(point, completion)
		m2CompletionRows = append(m2CompletionRows, completion)
	}
	m2Series := finishMonitoringPoints(m2Points)
	m2Summary := summarizeCompletions(m2CompletionRows)

	m3Points := newMonitoringPoints(from, until, bucket, m3Recorded)
	m3Chats := map[string]struct{}{}
	var processedMessages, todosCreated int64
	for _, row := range m3Starts {
		m3Chats[row.ChatID] = struct{}{}
		point := monitoringPointAt(m3Points, from, bucket, row.StartedAt)
		point.point.RunCount++
		point.scopes[row.ChatID] = struct{}{}
		if row.Status == "succeeded" {
			processedMessages += row.MessageCount
			todosCreated += row.TodoCount
		}
	}
	for _, row := range m3Completions {
		addCompletion(monitoringPointAt(m3Points, from, bucket, *row.FinishedAt), row)
	}
	m3Series := finishMonitoringPoints(m3Points)
	m3Summary := summarizeCompletions(m3Completions)

	m5Points := newMonitoringPoints(from, until, bucket, m5Recorded)
	m5Tasks := map[uint64]struct{}{}
	for _, row := range m5Starts {
		m5Tasks[row.TaskID] = struct{}{}
		point := monitoringPointAt(m5Points, from, bucket, row.StartedAt)
		point.point.RunCount++
		point.scopes[fmt.Sprint(row.TaskID)] = struct{}{}
	}
	for _, row := range m5Completions {
		addCompletion(monitoringPointAt(m5Points, from, bucket, *row.FinishedAt), row)
	}
	m5Series := finishMonitoringPoints(m5Points)
	m5Summary := summarizeCompletions(m5Completions)

	return &MonitoringSnapshot{
		From: from.Format(time.RFC3339Nano), Until: until.Format(time.RFC3339Nano), Bucket: bucket.String(),
		M2: CaptureMetrics{
			InsertedMessages: m2InsertedMessages, RunCount: int64(len(m2Starts)),
			AverageDurationMS: m2Summary.averageDuration, MaxDurationMS: m2Summary.maxDuration,
			FinishedRuns: m2Summary.finishedRuns, FailedRuns: m2Summary.failedRuns,
			FailureRate: m2Summary.failureRate, RecordedSince: formatRecordedSince(m2Recorded), Series: m2Series,
		},
		M3: ExtractionMetrics{
			ChatCount: int64(len(m3Chats)), RunCount: int64(len(m3Starts)),
			ProcessedMessages: processedMessages, TodosCreated: todosCreated,
			AverageDurationMS: m3Summary.averageDuration, MaxDurationMS: m3Summary.maxDuration,
			TotalTokens: m3Summary.totalTokens, TokenCoverageComplete: m3Summary.tokenCoverageComplete,
			FinishedRuns: m3Summary.finishedRuns, FailedRuns: m3Summary.failedRuns,
			FailureRate: m3Summary.failureRate, RecordedSince: formatRecordedSince(m3Recorded), Series: m3Series,
		},
		M5: ExecutionMetrics{
			ProcessedTasks: int64(len(m5Tasks)), RunCount: int64(len(m5Starts)),
			AverageDurationMS: m5Summary.averageDuration, MaxDurationMS: m5Summary.maxDuration,
			TotalTokens: m5Summary.totalTokens, TokenCoverageComplete: m5Summary.tokenCoverageComplete,
			FinishedRuns: m5Summary.finishedRuns, FailedRuns: m5Summary.failedRuns,
			FailureRate: m5Summary.failureRate, RecordedSince: formatRecordedSince(m5Recorded), Series: m5Series,
		},
	}, nil
}

func (s *DebugService) monitoringRecordedSince(ctx context.Context, model any) (*time.Time, error) {
	var row struct {
		StartedAt time.Time `gorm:"column:started_at"`
	}
	result := s.db.WithContext(ctx).Model(model).
		Select("started_at").Order("julianday(started_at) ASC").Limit(1).Scan(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &row.StartedAt, nil
}

func newMonitoringPoints(from, until time.Time, bucket time.Duration, recordedSince *time.Time) []*pointAccumulator {
	count := int((until.Sub(from) + bucket - 1) / bucket)
	points := make([]*pointAccumulator, count)
	for i := range points {
		start := from.Add(time.Duration(i) * bucket)
		active := recordedSince != nil && start.Add(bucket).After(recordedSince.UTC())
		points[i] = &pointAccumulator{
			point:  MonitoringPoint{BucketStart: start.Format(time.RFC3339Nano), RecordingActive: active},
			scopes: map[string]struct{}{},
		}
	}
	return points
}

func monitoringPointAt(points []*pointAccumulator, from time.Time, bucket time.Duration, at time.Time) *pointAccumulator {
	index := int(at.UTC().Sub(from) / bucket)
	return points[index]
}

func addCompletion(point *pointAccumulator, row completionRow) {
	point.point.FinishedRuns++
	if monitoringRunFailed(row.Status) {
		point.point.FailedRuns++
	}
	if row.DurationMS != nil {
		point.durationTotal += *row.DurationMS
		point.durationRuns++
	}
	if row.InputTokens != nil && row.OutputTokens != nil {
		point.tokenTotal += *row.InputTokens + *row.OutputTokens
		point.tokenReportedRuns++
	}
}

func finishMonitoringPoints(points []*pointAccumulator) []MonitoringPoint {
	out := make([]MonitoringPoint, len(points))
	for i, item := range points {
		item.point.ScopeCount += int64(len(item.scopes))
		if item.durationRuns > 0 {
			average := float64(item.durationTotal) / float64(item.durationRuns)
			item.point.AverageDurationMS = &average
		}
		if item.point.FinishedRuns > 0 {
			rate := float64(item.point.FailedRuns) / float64(item.point.FinishedRuns)
			item.point.FailureRate = &rate
			item.point.TokenCoverageComplete = item.tokenReportedRuns == item.point.FinishedRuns
		}
		if item.tokenReportedRuns > 0 {
			total := item.tokenTotal
			item.point.TotalTokens = &total
		}
		out[i] = item.point
	}
	return out
}

type completionSummary struct {
	averageDuration       *float64
	maxDuration           *int64
	totalTokens           *int64
	finishedRuns          int64
	failedRuns            int64
	failureRate           *float64
	tokenCoverageComplete bool
}

func summarizeCompletions(rows []completionRow) completionSummary {
	var summary completionSummary
	var durationTotal, durationRuns, tokenTotal, tokenReportedRuns int64
	for _, row := range rows {
		summary.finishedRuns++
		if monitoringRunFailed(row.Status) {
			summary.failedRuns++
		}
		if row.DurationMS != nil {
			durationTotal += *row.DurationMS
			durationRuns++
			if summary.maxDuration == nil || *row.DurationMS > *summary.maxDuration {
				value := *row.DurationMS
				summary.maxDuration = &value
			}
		}
		if row.InputTokens != nil && row.OutputTokens != nil {
			tokenTotal += *row.InputTokens + *row.OutputTokens
			tokenReportedRuns++
		}
	}
	if durationRuns > 0 {
		value := float64(durationTotal) / float64(durationRuns)
		summary.averageDuration = &value
	}
	if summary.finishedRuns > 0 {
		value := float64(summary.failedRuns) / float64(summary.finishedRuns)
		summary.failureRate = &value
		summary.tokenCoverageComplete = tokenReportedRuns == summary.finishedRuns
	}
	if tokenReportedRuns > 0 {
		value := tokenTotal
		summary.totalTokens = &value
	}
	return summary
}

func monitoringRunFailed(status string) bool {
	return status == "failed" || status == "error"
}

func formatRecordedSince(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}
