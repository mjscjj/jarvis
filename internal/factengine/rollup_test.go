package factengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type compressorCall struct {
	key        string
	system     string
	userPrompt string
}

type fakeCompressor struct {
	calls     []compressorCall
	responses [][]rollupResult
	errors    map[int]error
}

func (f *fakeCompressor) CompressRollups(_ context.Context, key, systemPrompt, userPrompt string) ([]rollupResult, error) {
	index := len(f.calls)
	f.calls = append(f.calls, compressorCall{key: key, system: systemPrompt, userPrompt: userPrompt})
	if err := f.errors[index]; err != nil {
		return nil, err
	}
	if index >= len(f.responses) {
		return nil, fmt.Errorf("no fake response for call %d", index+1)
	}
	return f.responses[index], nil
}

type fakeRollupContexts struct {
	background json.RawMessage
	calls      int
}

func (f *fakeRollupContexts) AssembleConversation(context.Context, contextsnap.AssembleOptions) (json.RawMessage, error) {
	f.calls++
	return append(json.RawMessage(nil), f.background...), nil
}

type fakeRollupPrompts struct {
	content string
}

func (f fakeRollupPrompts) Content(context.Context, string) (string, error) {
	return f.content, nil
}

type recordingAppender struct {
	db *gorm.DB
}

func (r *recordingAppender) AppendFact(_ context.Context, input progress.FactInput) (*progress.FactView, error) {
	row := domain.Fact{
		SubjectType: input.SubjectType, SubjectID: input.SubjectID,
		Description: input.Description, OccurredAt: input.OccurredAt.UTC(),
		SourceKind: input.SourceKind, SourceID: input.SourceID,
	}
	if err := r.db.Create(&row).Error; err != nil {
		return nil, err
	}
	view := progress.FactView{
		ID: row.ID, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
		Description: row.Description, OccurredAt: row.OccurredAt,
		SourceKind: row.SourceKind, SourceID: row.SourceID,
	}
	return &view, nil
}

func (r *recordingAppender) ListFacts(ctx context.Context, filter progress.FactFilter) ([]progress.FactView, error) {
	service, err := progress.NewService(r.db)
	if err != nil {
		return nil, err
	}
	return service.ListFacts(ctx, filter)
}

func newRollupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE fact (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject_type TEXT NOT NULL,
			subject_id INTEGER NOT NULL,
			description TEXT NOT NULL,
			occurred_at DATETIME NOT NULL,
			source_kind TEXT,
			source_id INTEGER,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE project (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	if err := db.Exec(`INSERT INTO project(id, name) VALUES (3, 'Jarvis')`).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return db
}

func seedRollupDetail(t *testing.T, db *gorm.DB, subjectType string, subjectID uint64, description string, occurredAt time.Time) {
	t.Helper()
	if err := db.Create(&domain.Fact{
		SubjectType: subjectType, SubjectID: subjectID,
		Description: description, OccurredAt: occurredAt,
	}).Error; err != nil {
		t.Fatalf("seed detail: %v", err)
	}
}

func newTestRollupWorker(t *testing.T, db *gorm.DB, compressor *fakeCompressor, contexts *fakeRollupContexts) *RollupWorker {
	t.Helper()
	worker, err := NewRollupWorker(db, compressor, &recordingAppender{db: db}, fakeRollupPrompts{content: "rollup system"}, contexts, time.UTC)
	if err != nil {
		t.Fatalf("NewRollupWorker: %v", err)
	}
	return worker
}

func TestRollupDayBatchesFiveSubjectsAndReusesOneBackground(t *testing.T) {
	t.Parallel()
	db := newRollupTestDB(t)
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for id := uint64(1); id <= 6; id++ {
		seedRollupDetail(t, db, "topic", id, fmt.Sprintf("detail-%d-late", id), day.Add(2*time.Hour))
	}
	seedRollupDetail(t, db, "topic", 1, "detail-1-early", day.Add(time.Hour))

	compressor := &fakeCompressor{responses: [][]rollupResult{
		{
			{SubjectType: "topic", SubjectID: 1, Description: "rollup-1"},
			{SubjectType: "topic", SubjectID: 2, Description: "rollup-2"},
			{SubjectType: "topic", SubjectID: 3, Description: "rollup-3"},
			{SubjectType: "topic", SubjectID: 4, Description: "rollup-4"},
			{SubjectType: "topic", SubjectID: 5, Description: "rollup-5"},
		},
		{{SubjectType: "topic", SubjectID: 6, Description: "rollup-6"}},
	}}
	contexts := &fakeRollupContexts{background: json.RawMessage(`{"captured_at":"2026-08-02T00:00:00Z","principal":{"name":"Principal"}}`)}
	worker := newTestRollupWorker(t, db, compressor, contexts)

	stats, err := worker.RollupDay(context.Background(), day)
	if err != nil {
		t.Fatalf("RollupDay: %v", err)
	}
	if stats.Subjects != 6 || stats.Batches != 2 || stats.FailedBatches != 0 || stats.Written != 6 {
		t.Fatalf("stats = %+v", stats)
	}
	if contexts.calls != 1 {
		t.Fatalf("context calls = %d, want 1", contexts.calls)
	}
	if len(compressor.calls) != 2 {
		t.Fatalf("compressor calls = %d, want 2", len(compressor.calls))
	}
	for _, call := range compressor.calls {
		if !strings.Contains(call.userPrompt, string(contexts.background)) {
			t.Fatalf("batch prompt does not contain shared background: %s", call.userPrompt)
		}
	}
	if got := compressor.calls[0].userPrompt; strings.Index(got, "detail-1-early") > strings.Index(got, "detail-1-late") {
		t.Fatalf("facts are not chronological: %s", got)
	}
}

func TestRollupSubjectDayOnlyReplacesRequestedSubject(t *testing.T) {
	db := newRollupTestDB(t)
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	seedRollupDetail(t, db, "topic", 1, "detail one", day.Add(time.Hour))
	seedRollupDetail(t, db, "topic", 2, "detail two", day.Add(2*time.Hour))
	compressor := &fakeCompressor{responses: [][]rollupResult{{
		{SubjectType: "topic", SubjectID: 2, Description: "only two"},
	}}}
	contexts := &fakeRollupContexts{background: json.RawMessage(`{"principal":{"name":"Principal"}}`)}
	worker := newTestRollupWorker(t, db, compressor, contexts)

	stats, err := worker.RollupSubjectDay(context.Background(), day, "topic", 2)
	if err != nil {
		t.Fatalf("RollupSubjectDay: %v", err)
	}
	if stats.Subjects != 1 || stats.Written != 1 || len(compressor.calls) != 1 {
		t.Fatalf("stats=%+v calls=%d", stats, len(compressor.calls))
	}
	var rollups []domain.Fact
	if err := db.Where("source_kind = ?", progress.FactSourceRollup).Find(&rollups).Error; err != nil {
		t.Fatal(err)
	}
	if len(rollups) != 1 || rollups[0].SubjectID != 2 || rollups[0].Description != "only two" {
		t.Fatalf("rollups = %#v", rollups)
	}
}

func TestRollupDayRejectsMismatchedSubjectsWithoutWriting(t *testing.T) {
	t.Parallel()
	db := newRollupTestDB(t)
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	seedRollupDetail(t, db, "topic", 1, "detail", day.Add(time.Hour))
	compressor := &fakeCompressor{responses: [][]rollupResult{{
		{SubjectType: "topic", SubjectID: 2, Description: "wrong subject"},
	}}}
	worker := newTestRollupWorker(t, db, compressor, &fakeRollupContexts{background: json.RawMessage(`{"principal":{}}`)})

	stats, err := worker.RollupDay(context.Background(), day)
	if err == nil || !strings.Contains(err.Error(), "unexpected subject=topic/2") {
		t.Fatalf("RollupDay error = %v", err)
	}
	if stats.Written != 0 || stats.FailedBatches != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	var count int64
	if err := db.Model(&domain.Fact{}).Where("source_kind = ?", progress.FactSourceRollup).Count(&count).Error; err != nil {
		t.Fatalf("count rollups: %v", err)
	}
	if count != 0 {
		t.Fatalf("rollup count = %d, want 0", count)
	}
}

func TestRollupDayContinuesAfterBatchFailure(t *testing.T) {
	t.Parallel()
	db := newRollupTestDB(t)
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for id := uint64(1); id <= 6; id++ {
		seedRollupDetail(t, db, "topic", id, fmt.Sprintf("detail-%d", id), day.Add(time.Hour))
	}
	compressor := &fakeCompressor{
		errors: map[int]error{0: errors.New("provider failed")},
		responses: [][]rollupResult{
			nil,
			{{SubjectType: "topic", SubjectID: 6, Description: "rollup-6"}},
		},
	}
	worker := newTestRollupWorker(t, db, compressor, &fakeRollupContexts{background: json.RawMessage(`{"principal":{}}`)})

	stats, err := worker.RollupDay(context.Background(), day)
	if err == nil || !strings.Contains(err.Error(), "provider failed") {
		t.Fatalf("RollupDay error = %v", err)
	}
	if stats.Batches != 2 || stats.FailedBatches != 1 || stats.Written != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	var rollup domain.Fact
	if err := db.Where("subject_type = ? AND subject_id = ? AND source_kind = ?", "topic", 6, progress.FactSourceRollup).First(&rollup).Error; err != nil {
		t.Fatalf("load later batch rollup: %v", err)
	}
}

func TestRollupDayIsIdempotent(t *testing.T) {
	t.Parallel()
	db := newRollupTestDB(t)
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	seedRollupDetail(t, db, "project", 3, "方案定了走 B", day.Add(3*time.Hour))

	compressor := &fakeCompressor{responses: [][]rollupResult{
		{{SubjectType: "project", SubjectID: 3, Description: "当天定了方案走 B。"}},
		{{SubjectType: "project", SubjectID: 3, Description: "重跑后的压缩：当天定了方案走 B。"}},
	}}
	worker := newTestRollupWorker(t, db, compressor, &fakeRollupContexts{background: json.RawMessage(`{"principal":{}}`)})

	first, err := worker.RollupDay(context.Background(), day)
	if err != nil || first.Written != 1 {
		t.Fatalf("first RollupDay stats=%+v error=%v", first, err)
	}
	second, err := worker.RollupDay(context.Background(), day)
	if err != nil || second.Written != 1 {
		t.Fatalf("second RollupDay stats=%+v error=%v", second, err)
	}

	var rollups []domain.Fact
	if err := db.Where("subject_type = ? AND subject_id = ? AND source_kind = ?", "project", 3, progress.FactSourceRollup).Find(&rollups).Error; err != nil {
		t.Fatalf("list rollups: %v", err)
	}
	if len(rollups) != 1 || rollups[0].Description != "重跑后的压缩：当天定了方案走 B。" {
		t.Fatalf("rollups = %#v", rollups)
	}
	var details int64
	if err := db.Model(&domain.Fact{}).
		Where("subject_type = ? AND subject_id = ? AND (source_kind IS NULL OR source_kind <> ?)", "project", 3, progress.FactSourceRollup).
		Count(&details).Error; err != nil {
		t.Fatalf("count details: %v", err)
	}
	if details != 1 {
		t.Fatalf("detail facts = %d, want 1", details)
	}
}
