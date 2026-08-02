package factengine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeCompressor struct {
	summary string
	calls   int
}

func (f *fakeCompressor) Compress(context.Context, string, string) (string, error) {
	f.calls++
	return f.summary, nil
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

func TestRollupDayIsIdempotent(t *testing.T) {
	t.Parallel()
	db := newRollupTestDB(t)
	loc := time.UTC
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)
	detailAt := day.Add(3 * time.Hour)
	if err := db.Create(&domain.Fact{
		SubjectType: "project", SubjectID: 3,
		Description: "方案定了走 B", OccurredAt: detailAt,
	}).Error; err != nil {
		t.Fatalf("seed detail: %v", err)
	}

	appender := &recordingAppender{db: db}
	compressor := &fakeCompressor{summary: "当天定了方案走 B。"}
	worker, err := NewRollupWorker(db, compressor, appender, fakePrompts{content: "rollup system"}, loc)
	if err != nil {
		t.Fatalf("NewRollupWorker: %v", err)
	}

	first, err := worker.RollupDay(context.Background(), day)
	if err != nil {
		t.Fatalf("first RollupDay: %v", err)
	}
	if first.Written != 1 {
		t.Fatalf("first written = %d, want 1", first.Written)
	}
	compressor.summary = "重跑后的压缩：当天定了方案走 B。"
	second, err := worker.RollupDay(context.Background(), day)
	if err != nil {
		t.Fatalf("second RollupDay: %v", err)
	}
	if second.Written != 1 {
		t.Fatalf("second written = %d, want 1", second.Written)
	}

	var rollups []domain.Fact
	if err := db.Where("subject_type = ? AND subject_id = ? AND source_kind = ?", "project", 3, progress.FactSourceRollup).
		Find(&rollups).Error; err != nil {
		t.Fatalf("list rollups: %v", err)
	}
	if len(rollups) != 1 {
		t.Fatalf("rollup count = %d, want 1 after rerun (idempotent replace)", len(rollups))
	}
	if rollups[0].Description != "重跑后的压缩：当天定了方案走 B。" {
		t.Fatalf("rollup description = %q", rollups[0].Description)
	}
	var details int64
	if err := db.Model(&domain.Fact{}).
		Where("subject_type = ? AND subject_id = ? AND (source_kind IS NULL OR source_kind <> ?)", "project", 3, progress.FactSourceRollup).
		Count(&details).Error; err != nil {
		t.Fatalf("count details: %v", err)
	}
	if details != 1 {
		t.Fatalf("detail facts = %d, want 1 (original must stay)", details)
	}
}
