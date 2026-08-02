package factengine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"jarvis/internal/progress"
	"jarvis/internal/textstore"
)

type cursorStore interface {
	Cursor(context.Context, string) (uint64, bool, error)
	AdvanceCursor(context.Context, string, uint64, time.Time) error
}

type factExtractor interface {
	Extract(context.Context, string, SourceUnit) ([]ExtractedFact, error)
}

// factAppender is progress.Service. Going through it rather than inserting rows
// directly is what keeps a hallucinated subject_id out of the table: it verifies
// the subject exists for the types the system reads back. ListFacts is shared by
// the daily rollup so compression reads the same filtered view the prompt does.
type factAppender interface {
	AppendFact(context.Context, progress.FactInput) (*progress.FactView, error)
	ListFacts(context.Context, progress.FactFilter) ([]progress.FactView, error)
}

type WorkerOptions struct {
	BatchLimit int
	Window     WindowOptions
	Prompts    textstore.Reader
}

type Stats struct {
	Units   int
	Facts   int
	Sources []SourceStats
}

// SourceStats keeps the independent progress of one material source visible in
// logs. A stalled Todo source must not make a successful Message round look as
// though it never happened.
type SourceStats struct {
	Source string
	Units  int
	Facts  int
	LastID uint64
	Seeded bool
}

// Worker runs one offline extraction round: read material above the watermark,
// distil each unit, store the facts, then move the watermark.
type Worker struct {
	store     cursorStore
	sources   []MaterialSource
	extractor factExtractor
	facts     factAppender
	opts      WorkerOptions
}

func NewWorker(store cursorStore, sources []MaterialSource, extractor factExtractor, facts factAppender, opts WorkerOptions) (*Worker, error) {
	if store == nil {
		return nil, fmt.Errorf("fact engine store is nil")
	}
	if extractor == nil {
		return nil, fmt.Errorf("fact engine extractor is nil")
	}
	if facts == nil {
		return nil, fmt.Errorf("fact engine fact appender is nil")
	}
	if opts.BatchLimit <= 0 {
		return nil, fmt.Errorf("fact engine batch limit must be positive")
	}
	if err := opts.Window.validate(); err != nil {
		return nil, err
	}
	if opts.Prompts == nil {
		return nil, fmt.Errorf("fact engine prompt reader is nil")
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("fact engine has no material sources")
	}
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source.Name == "" || source.MaxID == nil || source.Units == nil {
			return nil, fmt.Errorf("fact engine material source is incomplete: %+v", source)
		}
		if _, duplicate := seen[source.Name]; duplicate {
			return nil, fmt.Errorf("fact engine material source %q is duplicated", source.Name)
		}
		seen[source.Name] = struct{}{}
	}
	return &Worker{store: store, sources: append([]MaterialSource(nil), sources...), extractor: extractor, facts: facts, opts: opts}, nil
}

// ExtractOnce distils one batch from every registered material source.
//
// Each source owns an independent watermark. Sources whose units preserve id
// order checkpoint every successful unit; interleaved sources such as Message
// checkpoint only after the whole batch. Sources run sequentially, and errors
// are aggregated so one broken source does not starve the others.
func (w *Worker) ExtractOnce(ctx context.Context) (Stats, error) {
	stats := Stats{}
	var systemPrompt string
	var sourceErrors []error
	for _, source := range w.sources {
		sourceStats, err := w.extractSource(ctx, source, &systemPrompt)
		stats.Units += sourceStats.Units
		stats.Facts += sourceStats.Facts
		stats.Sources = append(stats.Sources, sourceStats)
		if err != nil {
			sourceErrors = append(sourceErrors, fmt.Errorf("source=%s: %w", source.Name, err))
			continue
		}
	}
	return stats, errors.Join(sourceErrors...)
}

func (w *Worker) extractSource(ctx context.Context, source MaterialSource, systemPrompt *string) (SourceStats, error) {
	stats := SourceStats{Source: source.Name}
	cursor, exists, err := w.store.Cursor(ctx, source.Name)
	if err != nil {
		return stats, err
	}
	if !exists && source.StartAtPresent {
		maxID, err := source.MaxID(ctx)
		if err != nil {
			return stats, err
		}
		if maxID == 0 {
			return stats, nil
		}
		if err := w.store.AdvanceCursor(ctx, source.Name, maxID, time.Time{}); err != nil {
			return stats, err
		}
		stats.LastID = maxID
		stats.Seeded = true
		return stats, nil
	}
	units, maxID, err := source.Units(ctx, cursor, w.opts.BatchLimit, w.opts.Window)
	if err != nil {
		return stats, err
	}
	if maxID == 0 {
		return stats, nil
	}
	if len(units) == 0 {
		return stats, fmt.Errorf("source returned max_id=%d with no material units", maxID)
	}
	if *systemPrompt == "" {
		*systemPrompt, err = w.opts.Prompts.Content(ctx, textstore.SystemPromptFactExtractKey)
		if err != nil {
			return stats, fmt.Errorf("read fact extraction system prompt: %w", err)
		}
	}
	for _, unit := range units {
		extracted, err := w.extractor.Extract(ctx, *systemPrompt, unit)
		if err != nil {
			return stats, err
		}
		stats.Units++
		stored, err := w.storeFacts(ctx, unit, extracted)
		if err != nil {
			return stats, err
		}
		stats.Facts += stored
		if source.CheckpointEachUnit {
			if err := w.store.AdvanceCursor(ctx, source.Name, unit.LastID, unit.OccurredAt); err != nil {
				return stats, err
			}
			stats.LastID = unit.LastID
		}
	}
	// Material that produced no facts still moves the watermark: "nothing here"
	// is a real answer, and re-reading it would cost the same tokens forever.
	if !source.CheckpointEachUnit {
		if err := w.store.AdvanceCursor(ctx, source.Name, maxID, latestOccurredAt(units)); err != nil {
			return stats, err
		}
		stats.LastID = maxID
	}
	return stats, nil
}

// storeFacts writes one unit's facts. SourceUnit.Subjects is context rather than
// an allowlist: the agent may resolve another real subject with tools, and the
// progress service validates the entity types it knows. Storage errors abort the
// round instead of being hidden behind a source-specific fallback.
func (w *Worker) storeFacts(ctx context.Context, unit SourceUnit, facts []ExtractedFact) (int, error) {
	source := unit.Source
	occurredAt := unit.OccurredAt
	stored := 0
	for _, fact := range facts {
		if _, err := w.facts.AppendFact(ctx, progress.FactInput{
			SubjectType: fact.SubjectType,
			SubjectID:   fact.SubjectID,
			Description: fact.Description,
			OccurredAt:  &occurredAt,
			SourceKind:  &source,
		}); err != nil {
			return stored, fmt.Errorf("store fact from unit=%s subject=%s/%d: %w",
				unit.Key, fact.SubjectType, fact.SubjectID, err)
		}
		stored++
	}
	return stored, nil
}

func latestOccurredAt(units []SourceUnit) time.Time {
	var latest time.Time
	for _, unit := range units {
		if unit.OccurredAt.After(latest) {
			latest = unit.OccurredAt
		}
	}
	return latest
}
