package factengine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/progress"
)

type fakeStore struct {
	cursor       uint64
	cursorSeeded bool
	maxMessageID uint64
	units        []SourceUnit
	maxID        uint64
	listErr      error
	advanced     []uint64
	advancedTime []time.Time
}

func (f *fakeStore) Cursor(context.Context, string) (uint64, bool, error) {
	return f.cursor, f.cursorSeeded, nil
}

func (f *fakeStore) MaxMessageID(context.Context) (uint64, error) { return f.maxMessageID, nil }

func (f *fakeStore) AdvanceCursor(_ context.Context, _ string, lastID uint64, occurredAt time.Time) error {
	f.advanced = append(f.advanced, lastID)
	f.advancedTime = append(f.advancedTime, occurredAt)
	return nil
}

func (f *fakeStore) MessageUnits(context.Context, uint64, int, WindowOptions) ([]SourceUnit, uint64, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.units, f.maxID, nil
}

type fakeExtractor struct {
	byUnit map[string][]ExtractedFact
	err    error
	calls  []string
}

func (f *fakeExtractor) Extract(_ context.Context, systemPrompt string, unit SourceUnit) ([]ExtractedFact, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return nil, fmt.Errorf("system prompt was not passed through")
	}
	f.calls = append(f.calls, unit.Key)
	if f.err != nil {
		return nil, f.err
	}
	return f.byUnit[unit.Key], nil
}

type fakeAppender struct {
	stored []progress.FactInput
	err    error
}

func (f *fakeAppender) AppendFact(_ context.Context, input progress.FactInput) (*progress.FactView, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.stored = append(f.stored, input)
	return &progress.FactView{ID: uint64(len(f.stored))}, nil
}

func (f *fakeAppender) ListFacts(context.Context, progress.FactFilter) ([]progress.FactView, error) {
	return nil, nil
}

type fakePrompts struct{ content string }

func (f fakePrompts) Content(context.Context, string) (string, error) { return f.content, nil }

func testUnit(key string, lastID uint64, occurredAt time.Time) SourceUnit {
	return SourceUnit{
		Source: SourceMessage, Key: key, LastID: lastID, OccurredAt: occurredAt,
		Body: "some conversation",
		Subjects: []Subject{
			{Type: "project", ID: 7, Name: "Jarvis"},
			{Type: "group", ID: 3, Name: "研发群"},
		},
	}
}

func newTestWorker(t *testing.T, store sourceStore, extractor factExtractor, facts factAppender) *Worker {
	t.Helper()
	worker, err := NewWorker(store, extractor, facts, WorkerOptions{
		BatchLimit: 100,
		Window:     WindowOptions{Gap: 30 * time.Minute, MaxMessages: 40, Location: time.UTC},
		Prompts:    fakePrompts{content: "记事实"},
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	return worker
}

func TestExtractOnceStoresFactsAndAdvancesCursor(t *testing.T) {
	occurredAt := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	store := &fakeStore{cursorSeeded: true, units: []SourceUnit{testUnit("chat-a:1-5", 5, occurredAt)}, maxID: 5}
	extractor := &fakeExtractor{byUnit: map[string][]ExtractedFact{
		"chat-a:1-5": {{SubjectType: "project", SubjectID: 7, Description: "定了用离线链路抽事实"}},
	}}
	facts := &fakeAppender{}

	stats, err := newTestWorker(t, store, extractor, facts).ExtractOnce(context.Background())
	if err != nil {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if stats.Units != 1 || stats.Facts != 1 || stats.LastID != 5 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(facts.stored) != 1 {
		t.Fatalf("stored facts = %d, want 1", len(facts.stored))
	}
	stored := facts.stored[0]
	if stored.SubjectType != "project" || stored.SubjectID != 7 {
		t.Fatalf("stored subject = %s/%d", stored.SubjectType, stored.SubjectID)
	}
	// The fact must land on the day the conversation happened, not on the day the
	// offline round got around to reading it.
	if stored.OccurredAt == nil || !stored.OccurredAt.Equal(occurredAt) {
		t.Fatalf("stored occurred_at = %v, want %v", stored.OccurredAt, occurredAt)
	}
	if stored.SourceKind == nil || *stored.SourceKind != SourceMessage {
		t.Fatalf("stored source_kind = %v, want %q", stored.SourceKind, SourceMessage)
	}
	if len(store.advanced) != 1 || store.advanced[0] != 5 {
		t.Fatalf("advanced cursor = %v, want [5]", store.advanced)
	}
}

// Material that yields nothing still moves the watermark: "nothing worth
// remembering here" is the common answer, and re-reading it would cost the same
// tokens on every round forever.
func TestExtractOnceAdvancesCursorWhenNoFactsFound(t *testing.T) {
	store := &fakeStore{cursorSeeded: true, units: []SourceUnit{testUnit("chat-a:1-5", 5, time.Now().UTC())}, maxID: 5}
	extractor := &fakeExtractor{byUnit: map[string][]ExtractedFact{}}
	facts := &fakeAppender{}

	stats, err := newTestWorker(t, store, extractor, facts).ExtractOnce(context.Background())
	if err != nil {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if stats.Facts != 0 || len(facts.stored) != 0 {
		t.Fatalf("stats = %+v stored = %d", stats, len(facts.stored))
	}
	if len(store.advanced) != 1 || store.advanced[0] != 5 {
		t.Fatalf("advanced cursor = %v, want [5]", store.advanced)
	}
}

func TestExtractOnceSkipsEmptyBatch(t *testing.T) {
	store := &fakeStore{cursorSeeded: true}
	extractor := &fakeExtractor{}
	facts := &fakeAppender{}

	stats, err := newTestWorker(t, store, extractor, facts).ExtractOnce(context.Background())
	if err != nil {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if stats != (Stats{}) {
		t.Fatalf("stats = %+v, want zero", stats)
	}
	if len(extractor.calls) != 0 || len(store.advanced) != 0 {
		t.Fatalf("calls = %v advanced = %v", extractor.calls, store.advanced)
	}
}

// Subjects surfaced by a source are context, not a protocol allowlist. An agent
// may resolve another subject with tools; the real progress service validates
// known entity types when it stores the fact.
func TestExtractOnceAcceptsSubjectOutsideSourceContext(t *testing.T) {
	store := &fakeStore{cursorSeeded: true, units: []SourceUnit{testUnit("chat-a:1-5", 5, time.Now().UTC())}, maxID: 5}
	extractor := &fakeExtractor{byUnit: map[string][]ExtractedFact{
		"chat-a:1-5": {
			{SubjectType: "person", SubjectID: 999, Description: "编出来的主体"},
			{SubjectType: "group", SubjectID: 3, Description: "群里定的口径"},
		},
	}}
	facts := &fakeAppender{}

	stats, err := newTestWorker(t, store, extractor, facts).ExtractOnce(context.Background())
	if err != nil {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if stats.Facts != 2 {
		t.Fatalf("stats = %+v, want both model-selected subjects stored", stats)
	}
	if len(facts.stored) != 2 || facts.stored[0].SubjectID != 999 || facts.stored[1].SubjectID != 3 {
		t.Fatalf("stored = %+v", facts.stored)
	}
}

// Case differences are the model's, not a different subject: the fact table
// lowercases subject_type on insert, so the offered-subject check has to match.
func TestExtractOnceMatchesSubjectTypeCaseInsensitively(t *testing.T) {
	store := &fakeStore{cursorSeeded: true, units: []SourceUnit{testUnit("chat-a:1-5", 5, time.Now().UTC())}, maxID: 5}
	extractor := &fakeExtractor{byUnit: map[string][]ExtractedFact{
		"chat-a:1-5": {{SubjectType: "Project", SubjectID: 7, Description: "大写也算同一个主体"}},
	}}
	facts := &fakeAppender{}

	stats, err := newTestWorker(t, store, extractor, facts).ExtractOnce(context.Background())
	if err != nil {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if stats.Facts != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

// A failed extraction must not move the watermark, or the material it never read
// is skipped forever.
func TestExtractOnceKeepsCursorWhenExtractionFails(t *testing.T) {
	store := &fakeStore{cursorSeeded: true, units: []SourceUnit{testUnit("chat-a:1-5", 5, time.Now().UTC())}, maxID: 5}
	extractor := &fakeExtractor{err: errors.New("model unavailable")}
	facts := &fakeAppender{}

	_, err := newTestWorker(t, store, extractor, facts).ExtractOnce(context.Background())
	if err == nil {
		t.Fatal("ExtractOnce() error = nil, want extraction failure")
	}
	if len(store.advanced) != 0 {
		t.Fatalf("advanced cursor = %v, want none", store.advanced)
	}
}

func TestExtractOnceKeepsCursorWhenStoringFails(t *testing.T) {
	store := &fakeStore{cursorSeeded: true, units: []SourceUnit{testUnit("chat-a:1-5", 5, time.Now().UTC())}, maxID: 5}
	extractor := &fakeExtractor{byUnit: map[string][]ExtractedFact{
		"chat-a:1-5": {{SubjectType: "group", SubjectID: 3, Description: "群里定的口径"}},
	}}
	facts := &fakeAppender{err: errors.New("subject not found")}

	_, err := newTestWorker(t, store, extractor, facts).ExtractOnce(context.Background())
	if err == nil {
		t.Fatal("ExtractOnce() error = nil, want storage failure")
	}
	if len(store.advanced) != 0 {
		t.Fatalf("advanced cursor = %v, want none", store.advanced)
	}
}

func TestNewWorkerRejectsIncompleteOptions(t *testing.T) {
	store := &fakeStore{}
	extractor := &fakeExtractor{}
	facts := &fakeAppender{}
	valid := WorkerOptions{
		BatchLimit: 100,
		Window:     WindowOptions{Gap: time.Minute, MaxMessages: 10, Location: time.UTC},
		Prompts:    fakePrompts{content: "x"},
	}
	tests := []struct {
		name    string
		mutate  func(*WorkerOptions)
		wantErr string
	}{
		{"batch limit", func(o *WorkerOptions) { o.BatchLimit = 0 }, "batch limit"},
		{"window gap", func(o *WorkerOptions) { o.Window.Gap = 0 }, "window gap"},
		{"window max", func(o *WorkerOptions) { o.Window.MaxMessages = 0 }, "window max messages"},
		{"location", func(o *WorkerOptions) { o.Window.Location = nil }, "location"},
		{"prompts", func(o *WorkerOptions) { o.Prompts = nil }, "prompt reader"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := valid
			tt.mutate(&opts)
			if _, err := NewWorker(store, extractor, facts, opts); err == nil ||
				!strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewWorker() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// Turning the engine on must not re-distil every message ever captured: a source
// with no watermark plants one at the present and reads nothing.
func TestExtractOnceSeedsCursorAtPresentOnFirstRun(t *testing.T) {
	store := &fakeStore{maxMessageID: 8421, units: []SourceUnit{testUnit("chat-a:1-5", 5, time.Now().UTC())}, maxID: 5}
	extractor := &fakeExtractor{}
	facts := &fakeAppender{}

	stats, err := newTestWorker(t, store, extractor, facts).ExtractOnce(context.Background())
	if err != nil {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if !stats.Seeded || stats.LastID != 8421 || stats.Units != 0 {
		t.Fatalf("stats = %+v, want a seed-only round at 8421", stats)
	}
	if len(extractor.calls) != 0 {
		t.Fatalf("extractor calls = %v, want none on the seeding round", extractor.calls)
	}
	if len(store.advanced) != 1 || store.advanced[0] != 8421 {
		t.Fatalf("advanced cursor = %v, want [8421]", store.advanced)
	}
}

// An empty message table has no present to seed at, so the source stays unseeded
// and tries again next round instead of pinning itself at zero.
func TestExtractOnceLeavesCursorUnseededWhenNoMessages(t *testing.T) {
	store := &fakeStore{}
	stats, err := newTestWorker(t, store, &fakeExtractor{}, &fakeAppender{}).ExtractOnce(context.Background())
	if err != nil {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if stats != (Stats{}) {
		t.Fatalf("stats = %+v, want zero", stats)
	}
	if len(store.advanced) != 0 {
		t.Fatalf("advanced cursor = %v, want none", store.advanced)
	}
}
