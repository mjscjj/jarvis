package factengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/observability"
	"jarvis/internal/progress"
	"jarvis/internal/textstore"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

const rollupSubjectsPerBatch = 5

// rollupCompressor turns one batch of at most five subjects into one result per
// subject. The real implementation calls the agent CLI once; tests inject a stub.
type rollupCompressor interface {
	CompressRollups(ctx context.Context, key, systemPrompt, userPrompt string) ([]rollupResult, error)
}

type rollupFactStore interface {
	AppendFact(context.Context, progress.FactInput) (*progress.FactView, error)
	ListFacts(context.Context, progress.FactFilter) ([]progress.FactView, error)
}

type rollupContextAssembler interface {
	AssembleConversation(context.Context, contextsnap.AssembleOptions) (json.RawMessage, error)
}

// RollupStats reports what one compression round did.
type RollupStats struct {
	Subjects      int
	Batches       int
	FailedBatches int
	Written       int
	Skipped       int
	Day           string // YYYY-MM-DD in the worker's location
}

// RollupWorker compresses the previous local day's detail facts into one
// source_kind=rollup fact per subject. It shares the factengine package's db
// handle and scheduling pattern; it is a side channel, not part of the extract
// watermark path.
type RollupWorker struct {
	db         *gorm.DB
	compressor rollupCompressor
	facts      rollupFactStore
	prompts    textstore.Reader
	contexts   rollupContextAssembler
	location   *time.Location
	now        func() time.Time
}

// NewRollupWorker builds a compression worker. location owns the natural-day
// boundary (same rule as OccurredAt: callers pick the zone, the server does not).
func NewRollupWorker(db *gorm.DB, compressor rollupCompressor, facts rollupFactStore, prompts textstore.Reader, contexts rollupContextAssembler, location *time.Location) (*RollupWorker, error) {
	if db == nil {
		return nil, fmt.Errorf("fact rollup db is nil")
	}
	if compressor == nil {
		return nil, fmt.Errorf("fact rollup compressor is nil")
	}
	if facts == nil {
		return nil, fmt.Errorf("fact rollup fact appender is nil")
	}
	if prompts == nil {
		return nil, fmt.Errorf("fact rollup prompt reader is nil")
	}
	if contexts == nil {
		return nil, fmt.Errorf("fact rollup context assembler is nil")
	}
	if location == nil {
		return nil, fmt.Errorf("fact rollup location is nil")
	}
	return &RollupWorker{db: db, compressor: compressor, facts: facts, prompts: prompts, contexts: contexts, location: location, now: time.Now}, nil
}

// StartRollupScheduler runs one non-overlapping rollup round on the configured
// cadence. Same SkipIfStillRunning chain as StartScheduler.
func StartRollupScheduler(ctx context.Context, worker *RollupWorker, spec string, logger *log.Logger) (*cron.Cron, error) {
	if worker == nil {
		return nil, fmt.Errorf("fact rollup scheduler worker is nil")
	}
	if spec == "" {
		return nil, fmt.Errorf("fact rollup scheduler spec is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("fact rollup scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	))
	if _, err := scheduler.AddFunc(spec, func() {
		jobCtx := observability.EnsureLogID(ctx)
		startedAt := time.Now()
		stats, err := worker.RollupPreviousDay(jobCtx)
		if err != nil {
			logger.Printf(
				"logid=%s job=fact_rollup status=error duration_ms=%d day=%s subjects=%d batches=%d failed_batches=%d written=%d skipped=%d error=%+v",
				observability.LogID(jobCtx), time.Since(startedAt).Milliseconds(), stats.Day, stats.Subjects, stats.Batches, stats.FailedBatches, stats.Written, stats.Skipped, err,
			)
			return
		}
		logger.Printf(
			"logid=%s job=fact_rollup status=ok duration_ms=%d day=%s subjects=%d batches=%d failed_batches=%d written=%d skipped=%d",
			observability.LogID(jobCtx), time.Since(startedAt).Milliseconds(), stats.Day, stats.Subjects, stats.Batches, stats.FailedBatches, stats.Written, stats.Skipped,
		)
	}); err != nil {
		return nil, fmt.Errorf("register fact rollup job schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}

// RollupPreviousDay compresses yesterday in the worker's location.
func (w *RollupWorker) RollupPreviousDay(ctx context.Context) (RollupStats, error) {
	now := w.now().In(w.location)
	yesterday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, w.location).AddDate(0, 0, -1)
	return w.RollupDay(ctx, yesterday)
}

// RollupDay compresses one natural day whose local midnight is dayStart.
// dayStart must already be in the worker's location at 00:00; the half-open
// window is [dayStart, dayStart+24h).
func (w *RollupWorker) RollupDay(ctx context.Context, dayStart time.Time) (RollupStats, error) {
	return w.rollupDay(ctx, dayStart, nil)
}

// RollupSubjectDay recomputes exactly one subject's rollup for one natural day.
// It uses the same compression path as the scheduled full-day job; the narrower
// entry point exists so the UI can repair a stale or missing subject without
// rerunning every other subject for that day.
func (w *RollupWorker) RollupSubjectDay(ctx context.Context, dayStart time.Time, subjectType string, subjectID uint64) (RollupStats, error) {
	subjectType = strings.TrimSpace(strings.ToLower(subjectType))
	if subjectType == "" || subjectID == 0 {
		return RollupStats{}, fmt.Errorf("subject_type and positive subject_id are required")
	}
	subject := rollupSubject{Type: subjectType, ID: subjectID}
	return w.rollupDay(ctx, dayStart, &subject)
}

func (w *RollupWorker) rollupDay(ctx context.Context, dayStart time.Time, requested *rollupSubject) (RollupStats, error) {
	dayStart = time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 0, 0, 0, 0, w.location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	stats := RollupStats{Day: dayStart.Format("2006-01-02")}

	subjects, err := w.subjectsWithDetails(ctx, dayStart, dayEnd)
	if err != nil {
		return stats, err
	}
	if requested != nil {
		matched := subjects[:0]
		for _, subject := range subjects {
			if subject == *requested {
				matched = append(matched, subject)
			}
		}
		subjects = matched
	}
	stats.Subjects = len(subjects)
	if len(subjects) == 0 {
		return stats, nil
	}

	systemPrompt, err := w.prompts.Content(ctx, textstore.SystemPromptFactRollupKey)
	if err != nil {
		return stats, fmt.Errorf("read fact rollup system prompt: %w", err)
	}
	background, err := w.contexts.AssembleConversation(ctx, contextsnap.AssembleOptions{})
	if err != nil {
		return stats, fmt.Errorf("assemble fact rollup background: %w", err)
	}
	if len(background) == 0 || !json.Valid(background) {
		return stats, fmt.Errorf("assemble fact rollup background: invalid JSON")
	}

	var batchErrors []error
	for start := 0; start < len(subjects); start += rollupSubjectsPerBatch {
		end := min(start+rollupSubjectsPerBatch, len(subjects))
		stats.Batches++
		jobs, err := w.loadRollupJobs(ctx, subjects[start:end], dayStart, dayEnd, &stats)
		if err != nil {
			stats.FailedBatches++
			batchErrors = append(batchErrors, fmt.Errorf("batch=%d: %w", stats.Batches, err))
			continue
		}
		if len(jobs) == 0 {
			continue
		}
		key := fmt.Sprintf("rollup:%s:batch-%d", stats.Day, stats.Batches)
		results, err := w.compressor.CompressRollups(ctx, key, systemPrompt, buildRollupUserPrompt(background, stats.Day, jobs))
		if err != nil {
			stats.FailedBatches++
			batchErrors = append(batchErrors, fmt.Errorf("batch=%d: compress day=%s: %w", stats.Batches, stats.Day, err))
			continue
		}
		descriptions, err := validateRollupResults(jobs, results)
		if err != nil {
			stats.FailedBatches++
			batchErrors = append(batchErrors, fmt.Errorf("batch=%d: validate output day=%s: %w", stats.Batches, stats.Day, err))
			continue
		}
		for _, job := range jobs {
			if err := w.replaceRollup(ctx, job.Subject, dayStart, dayEnd, descriptions[rollupSubjectKey(job.Subject)]); err != nil {
				stats.FailedBatches++
				batchErrors = append(batchErrors, fmt.Errorf("batch=%d: %w", stats.Batches, err))
				break
			}
			stats.Written++
		}
	}
	return stats, errors.Join(batchErrors...)
}

type rollupSubject struct {
	Type string
	ID   uint64
}

type rollupJob struct {
	Subject rollupSubject
	Name    string
	Details []progress.FactView
}

type rollupResult struct {
	SubjectType string `json:"subject_type"`
	SubjectID   uint64 `json:"subject_id"`
	Description string `json:"description"`
}

type rollupResponse struct {
	Rollups *[]rollupResult `json:"rollups"`
}

func (w *RollupWorker) loadRollupJobs(ctx context.Context, subjects []rollupSubject, from, until time.Time, stats *RollupStats) ([]rollupJob, error) {
	jobs := make([]rollupJob, 0, len(subjects))
	for _, subject := range subjects {
		details, err := w.facts.ListFacts(ctx, progress.FactFilter{
			SubjectType:       subject.Type,
			SubjectID:         subject.ID,
			From:              &from,
			Until:             &until,
			ExcludeSourceKind: strPtr(progress.FactSourceRollup),
		})
		if err != nil {
			return nil, fmt.Errorf("list detail facts subject=%s/%d day=%s: %w", subject.Type, subject.ID, stats.Day, err)
		}
		if len(details) == 0 {
			stats.Skipped++
			continue
		}
		for left, right := 0, len(details)-1; left < right; left, right = left+1, right-1 {
			details[left], details[right] = details[right], details[left]
		}
		name, err := w.subjectName(ctx, subject)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, rollupJob{Subject: subject, Name: name, Details: details})
	}
	return jobs, nil
}

func (w *RollupWorker) subjectsWithDetails(ctx context.Context, from, until time.Time) ([]rollupSubject, error) {
	var rows []rollupSubject
	err := w.db.WithContext(ctx).Model(&domain.Fact{}).
		Select("DISTINCT subject_type AS type, subject_id AS id").
		Where("occurred_at >= ? AND occurred_at < ?", from.UTC(), until.UTC()).
		Where("(source_kind IS NULL OR source_kind <> ?)", progress.FactSourceRollup).
		Order("subject_type ASC, subject_id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list rollup subjects from=%s until=%s: %w", from.Format(time.RFC3339), until.Format(time.RFC3339), err)
	}
	return rows, nil
}

func (w *RollupWorker) subjectName(ctx context.Context, subject rollupSubject) (string, error) {
	switch subject.Type {
	case "project":
		var row domain.Project
		if err := w.db.WithContext(ctx).Select("id", "name").First(&row, subject.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Sprintf("project/%d", subject.ID), nil
			}
			return "", fmt.Errorf("load project name id=%d: %w", subject.ID, err)
		}
		return row.Name, nil
	case "group":
		var row domain.Group
		if err := w.db.WithContext(ctx).Select("id", "name").First(&row, subject.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Sprintf("group/%d", subject.ID), nil
			}
			return "", fmt.Errorf("load group name id=%d: %w", subject.ID, err)
		}
		if row.Name != nil && strings.TrimSpace(*row.Name) != "" {
			return *row.Name, nil
		}
		return fmt.Sprintf("group/%d", subject.ID), nil
	case "person":
		var row domain.Person
		if err := w.db.WithContext(ctx).Select("id", "name").First(&row, subject.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Sprintf("person/%d", subject.ID), nil
			}
			return "", fmt.Errorf("load person name id=%d: %w", subject.ID, err)
		}
		return row.Name, nil
	default:
		return fmt.Sprintf("%s/%d", subject.Type, subject.ID), nil
	}
}

// replaceRollup deletes any existing rollup for the subject/day, then writes a
// fresh one. No transaction: fail-fast mid-way and the next run replaces again
// (AGENTS.md §5).
func (w *RollupWorker) replaceRollup(ctx context.Context, subject rollupSubject, from, until time.Time, description string) error {
	result := w.db.WithContext(ctx).
		Where("subject_type = ? AND subject_id = ?", subject.Type, subject.ID).
		Where("occurred_at >= ? AND occurred_at < ?", from.UTC(), until.UTC()).
		Where("source_kind = ?", progress.FactSourceRollup).
		Delete(&domain.Fact{})
	if result.Error != nil {
		return fmt.Errorf("delete existing rollup subject=%s/%d: %w", subject.Type, subject.ID, result.Error)
	}
	kind := progress.FactSourceRollup
	occurred := from
	if _, err := w.facts.AppendFact(ctx, progress.FactInput{
		SubjectType: subject.Type,
		SubjectID:   subject.ID,
		Description: description,
		OccurredAt:  &occurred,
		SourceKind:  &kind,
	}); err != nil {
		return fmt.Errorf("append rollup subject=%s/%d: %w", subject.Type, subject.ID, err)
	}
	return nil
}

func buildRollupUserPrompt(background json.RawMessage, day string, jobs []rollupJob) string {
	lines := make([]string, 0, 8)
	lines = append(lines,
		"FACT_DAY="+day,
		"BEGIN_DAILY_BACKGROUND",
		string(background),
		"END_DAILY_BACKGROUND",
		"",
		"ROLLUP_SUBJECTS:",
	)
	for _, job := range jobs {
		lines = append(lines,
			"BEGIN_SUBJECT",
			fmt.Sprintf("SUBJECT_TYPE=%s SUBJECT_ID=%d SUBJECT_NAME=%q", job.Subject.Type, job.Subject.ID, job.Name),
			"DETAIL_FACTS:",
		)
		for _, fact := range job.Details {
			lines = append(lines, fmt.Sprintf("- [%s] %s", fact.OccurredAt.Format(time.RFC3339), fact.Description))
		}
		lines = append(lines, "END_SUBJECT", "")
	}
	return strings.Join(lines, "\n")
}

func validateRollupResults(jobs []rollupJob, results []rollupResult) (map[string]string, error) {
	expected := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		expected[rollupSubjectKey(job.Subject)] = struct{}{}
	}
	if len(results) != len(expected) {
		return nil, fmt.Errorf("rollup count=%d, want %d", len(results), len(expected))
	}
	descriptions := make(map[string]string, len(results))
	for i, result := range results {
		result.SubjectType = strings.TrimSpace(strings.ToLower(result.SubjectType))
		result.Description = strings.TrimSpace(result.Description)
		if result.SubjectType == "" || result.SubjectID == 0 || result.Description == "" {
			return nil, fmt.Errorf("rollup %d needs subject_type, positive subject_id and description", i+1)
		}
		key := rollupSubjectKey(rollupSubject{Type: result.SubjectType, ID: result.SubjectID})
		if _, ok := expected[key]; !ok {
			return nil, fmt.Errorf("rollup %d returned unexpected subject=%s/%d", i+1, result.SubjectType, result.SubjectID)
		}
		if _, exists := descriptions[key]; exists {
			return nil, fmt.Errorf("rollup %d duplicated subject=%s/%d", i+1, result.SubjectType, result.SubjectID)
		}
		descriptions[key] = result.Description
	}
	for key := range expected {
		if _, ok := descriptions[key]; !ok {
			return nil, fmt.Errorf("missing rollup subject=%s", key)
		}
	}
	return descriptions, nil
}

func rollupSubjectKey(subject rollupSubject) string {
	return fmt.Sprintf("%s/%d", strings.TrimSpace(strings.ToLower(subject.Type)), subject.ID)
}

func strPtr(value string) *string { return &value }

// CompressRollups runs the agent CLI exactly once for one batch. Malformed JSON
// is returned as an error without an automatic retry.
func (e *Extractor) CompressRollups(ctx context.Context, key, systemPrompt, userPrompt string) ([]rollupResult, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return nil, fmt.Errorf("fact rollup system prompt is empty")
	}
	if strings.TrimSpace(userPrompt) == "" {
		return nil, fmt.Errorf("fact rollup user prompt is empty")
	}
	raw, err := e.run(ctx, key, systemPrompt+"\n\n"+userPrompt)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("fact rollup response is empty")
	}
	var response rollupResponse
	if err := json.Unmarshal([]byte(trimmed), &response); err != nil {
		return nil, fmt.Errorf("parse fact rollup response: %w", err)
	}
	if response.Rollups == nil {
		return nil, fmt.Errorf("fact rollup response has no rollups key")
	}
	return *response.Rollups, nil
}
