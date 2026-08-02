package factengine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/observability"
	"jarvis/internal/progress"
	"jarvis/internal/textstore"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// rollupCompressor turns one subject's detail facts for a day into a single
// prose paragraph. The real implementation calls the agent CLI; tests inject a
// stub.
type rollupCompressor interface {
	Compress(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// RollupStats reports what one compression round did.
type RollupStats struct {
	Subjects int
	Written  int
	Skipped  int
	Day      string // YYYY-MM-DD in the worker's location
}

// RollupWorker compresses the previous local day's detail facts into one
// source_kind=rollup fact per subject. It shares the factengine package's db
// handle and scheduling pattern; it is a side channel, not part of the extract
// watermark path.
type RollupWorker struct {
	db         *gorm.DB
	compressor rollupCompressor
	facts      factAppender
	prompts    textstore.Reader
	location   *time.Location
	now        func() time.Time
}

// NewRollupWorker builds a compression worker. location owns the natural-day
// boundary (same rule as OccurredAt: callers pick the zone, the server does not).
func NewRollupWorker(db *gorm.DB, compressor rollupCompressor, facts factAppender, prompts textstore.Reader, location *time.Location) (*RollupWorker, error) {
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
	if location == nil {
		return nil, fmt.Errorf("fact rollup location is nil")
	}
	return &RollupWorker{db: db, compressor: compressor, facts: facts, prompts: prompts, location: location, now: time.Now}, nil
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
		stats, err := worker.RollupPreviousDay(jobCtx)
		if err != nil {
			logger.Printf("logid=%s job=fact_rollup status=error error=%+v", observability.LogID(jobCtx), err)
			return
		}
		logger.Printf(
			"logid=%s job=fact_rollup status=ok day=%s subjects=%d written=%d skipped=%d",
			observability.LogID(jobCtx), stats.Day, stats.Subjects, stats.Written, stats.Skipped,
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
	dayStart = time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 0, 0, 0, 0, w.location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	stats := RollupStats{Day: dayStart.Format("2006-01-02")}

	subjects, err := w.subjectsWithDetails(ctx, dayStart, dayEnd)
	if err != nil {
		return stats, err
	}
	stats.Subjects = len(subjects)
	if len(subjects) == 0 {
		return stats, nil
	}

	systemPrompt, err := w.prompts.Content(ctx, textstore.SystemPromptFactRollupKey)
	if err != nil {
		return stats, fmt.Errorf("read fact rollup system prompt: %w", err)
	}

	for _, subject := range subjects {
		details, err := w.facts.ListFacts(ctx, progress.FactFilter{
			SubjectType:       subject.Type,
			SubjectID:         subject.ID,
			From:              &dayStart,
			Until:             &dayEnd,
			ExcludeSourceKind: strPtr(progress.FactSourceRollup),
		})
		if err != nil {
			return stats, fmt.Errorf("list detail facts subject=%s/%d day=%s: %w", subject.Type, subject.ID, stats.Day, err)
		}
		if len(details) == 0 {
			stats.Skipped++
			continue
		}
		name, err := w.subjectName(ctx, subject)
		if err != nil {
			return stats, err
		}
		userPrompt := buildRollupUserPrompt(subject, name, stats.Day, details)
		summary, err := w.compressor.Compress(ctx, systemPrompt, userPrompt)
		if err != nil {
			return stats, fmt.Errorf("compress subject=%s/%d day=%s: %w", subject.Type, subject.ID, stats.Day, err)
		}
		summary = strings.TrimSpace(summary)
		if summary == "" {
			return stats, fmt.Errorf("compress subject=%s/%d day=%s: empty summary", subject.Type, subject.ID, stats.Day)
		}
		if err := w.replaceRollup(ctx, subject, dayStart, dayEnd, summary); err != nil {
			return stats, err
		}
		stats.Written++
	}
	return stats, nil
}

type rollupSubject struct {
	Type string
	ID   uint64
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

func buildRollupUserPrompt(subject rollupSubject, name, day string, details []progress.FactView) string {
	lines := make([]string, 0, len(details)+4)
	lines = append(lines,
		fmt.Sprintf("SUBJECT_TYPE=%s SUBJECT_ID=%d SUBJECT_NAME=%q DAY=%s", subject.Type, subject.ID, name, day),
		"",
		"DETAIL_FACTS:",
	)
	for _, fact := range details {
		lines = append(lines, fmt.Sprintf("- [%s] %s", fact.OccurredAt.Format(time.RFC3339), fact.Description))
	}
	return strings.Join(lines, "\n")
}

func strPtr(value string) *string { return &value }

// Compress runs the agent CLI once and returns the trimmed last message as the
// rollup paragraph. Reuses Extractor's binary/model/sandbox/timeout.
func (e *Extractor) Compress(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return "", fmt.Errorf("fact rollup system prompt is empty")
	}
	if strings.TrimSpace(userPrompt) == "" {
		return "", fmt.Errorf("fact rollup user prompt is empty")
	}
	raw, err := e.run(ctx, SourceUnit{Key: "rollup"}, systemPrompt+"\n\n"+userPrompt)
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(string(raw))
	if summary == "" {
		return "", fmt.Errorf("fact rollup response is empty")
	}
	return summary, nil
}
