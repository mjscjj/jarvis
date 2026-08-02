package factengine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/progress"
	"jarvis/internal/textstore"
)

type sourceStore interface {
	Cursor(context.Context, string) (uint64, bool, error)
	MaxMessageID(context.Context) (uint64, error)
	AdvanceCursor(context.Context, string, uint64, time.Time) error
	MessageUnits(context.Context, uint64, int, WindowOptions) ([]SourceUnit, uint64, error)
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
	Units    int
	Facts    int
	LastID   uint64
	Rejected int
	// Seeded reports that this round only planted the source's starting
	// watermark and read no material.
	Seeded bool
}

// Worker runs one offline extraction round: read material above the watermark,
// distil each unit, store the facts, then move the watermark.
type Worker struct {
	store     sourceStore
	extractor factExtractor
	facts     factAppender
	opts      WorkerOptions
}

func NewWorker(store sourceStore, extractor factExtractor, facts factAppender, opts WorkerOptions) (*Worker, error) {
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
	return &Worker{store: store, extractor: extractor, facts: facts, opts: opts}, nil
}

// ExtractOnce distils one batch of messages.
//
// The watermark moves once, after every unit in the batch succeeded, because a
// batch spans several chats whose ids interleave: committing per unit would
// leave a lower-id window of another chat stranded below the watermark. So a
// failed round advances nothing and the whole batch is retried, which can write
// a fact twice when the failure came after some units had already stored theirs.
// That is the accepted trade: a duplicate fact is something the consolidation
// step can merge, a lost one is gone.
// A source that has never run starts at the present. Reading from id 0 would
// re-distil every message ever captured, which is a deliberate backfill decision
// (lower the cursor row by hand), not the default behaviour of turning the engine
// on.
func (w *Worker) ExtractOnce(ctx context.Context) (Stats, error) {
	cursor, seeded, err := w.store.Cursor(ctx, SourceMessage)
	if err != nil {
		return Stats{}, err
	}
	if !seeded {
		return w.seedCursor(ctx)
	}
	units, maxID, err := w.store.MessageUnits(ctx, cursor, w.opts.BatchLimit, w.opts.Window)
	if err != nil {
		return Stats{}, err
	}
	if maxID == 0 {
		return Stats{}, nil
	}
	systemPrompt, err := w.opts.Prompts.Content(ctx, textstore.SystemPromptFactExtractKey)
	if err != nil {
		return Stats{}, fmt.Errorf("read fact extraction system prompt: %w", err)
	}
	stats := Stats{}
	for _, unit := range units {
		extracted, err := w.extractor.Extract(ctx, systemPrompt, unit)
		if err != nil {
			return stats, err
		}
		stats.Units++
		stored, rejected, err := w.storeFacts(ctx, unit, extracted)
		if err != nil {
			return stats, err
		}
		stats.Facts += stored
		stats.Rejected += rejected
	}
	// Material that produced no facts still moves the watermark: "nothing here"
	// is a real answer, and re-reading it would cost the same tokens forever.
	if err := w.store.AdvanceCursor(ctx, SourceMessage, maxID, latestOccurredAt(units)); err != nil {
		return stats, err
	}
	stats.LastID = maxID
	return stats, nil
}

// seedCursor plants the starting watermark at the newest captured message. An
// empty message table plants nothing, so the next round tries again rather than
// pinning the source at zero.
func (w *Worker) seedCursor(ctx context.Context) (Stats, error) {
	maxID, err := w.store.MaxMessageID(ctx)
	if err != nil {
		return Stats{}, err
	}
	if maxID == 0 {
		return Stats{}, nil
	}
	if err := w.store.AdvanceCursor(ctx, SourceMessage, maxID, time.Time{}); err != nil {
		return Stats{}, err
	}
	return Stats{LastID: maxID, Seeded: true}, nil
}

// storeFacts writes one unit's facts. A fact bound to a subject the model was
// not offered is dropped with a count rather than failing the round: the subject
// list is in the prompt, and one bad binding should not park every other fact in
// the batch. Any other storage failure is a real fault and aborts.
func (w *Worker) storeFacts(ctx context.Context, unit SourceUnit, facts []ExtractedFact) (int, int, error) {
	offered := make(map[string]struct{}, len(unit.Subjects))
	for _, subject := range unit.Subjects {
		offered[subjectKey(subject.Type, subject.ID)] = struct{}{}
	}
	source := unit.Source
	occurredAt := unit.OccurredAt
	stored, rejected := 0, 0
	for _, fact := range facts {
		if _, ok := offered[subjectKey(fact.SubjectType, fact.SubjectID)]; !ok {
			rejected++
			continue
		}
		if _, err := w.facts.AppendFact(ctx, progress.FactInput{
			SubjectType: fact.SubjectType,
			SubjectID:   fact.SubjectID,
			Description: fact.Description,
			OccurredAt:  &occurredAt,
			SourceKind:  &source,
		}); err != nil {
			return stored, rejected, fmt.Errorf("store fact from unit=%s subject=%s/%d: %w",
				unit.Key, fact.SubjectType, fact.SubjectID, err)
		}
		stored++
	}
	return stored, rejected, nil
}

// subjectKey matches the offered subjects case-insensitively, the same way the
// fact table stores subject_type (lowercased on insert).
func subjectKey(subjectType string, id uint64) string {
	return fmt.Sprintf("%s/%d", strings.ToLower(strings.TrimSpace(subjectType)), id)
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
