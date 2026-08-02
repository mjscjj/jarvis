package progress

import (
	"context"
	"fmt"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newFactTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE fact (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject_type TEXT NOT NULL,
			subject_id INTEGER NOT NULL,
			description TEXT NOT NULL,
			occurred_at DATETIME NOT NULL,
			source_kind TEXT,
			source_id INTEGER,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`).Error; err != nil {
		t.Fatalf("create fact table: %v", err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func insertFact(t *testing.T, service *Service, subjectType string, subjectID uint64, description string, occurredAt time.Time, sourceKind *string) {
	t.Helper()
	row := domain.Fact{
		SubjectType: subjectType, SubjectID: subjectID,
		Description: description, OccurredAt: occurredAt.UTC(),
		SourceKind: sourceKind,
	}
	if err := service.db.Create(&row).Error; err != nil {
		t.Fatalf("insert fact: %v", err)
	}
}

func TestListFactsSourceKindEqualityAndExclusion(t *testing.T) {
	t.Parallel()
	service := newFactTestService(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	rollup := FactSourceRollup
	m3 := "m3"
	insertFact(t, service, "group", 7, "明细 A", day, &m3)
	insertFact(t, service, "group", 7, "明细 NULL source", day.Add(time.Hour), nil)
	insertFact(t, service, "group", 7, "rollup 当天", day.Add(2*time.Hour), &rollup)

	equal, err := service.ListFacts(ctx, FactFilter{
		SubjectType: "group", SubjectID: 7, SourceKind: &rollup,
	})
	if err != nil {
		t.Fatalf("ListFacts SourceKind=rollup: %v", err)
	}
	if len(equal) != 1 || equal[0].Description != "rollup 当天" {
		t.Fatalf("SourceKind equality = %#v, want only rollup", equal)
	}

	excluded, err := service.ListFacts(ctx, FactFilter{
		SubjectType: "group", SubjectID: 7, ExcludeSourceKind: &rollup,
	})
	if err != nil {
		t.Fatalf("ListFacts ExcludeSourceKind=rollup: %v", err)
	}
	if len(excluded) != 2 {
		t.Fatalf("ExcludeSourceKind count = %d, want 2 (NULL must pass): %#v", len(excluded), excluded)
	}
	for _, fact := range excluded {
		if fact.SourceKind != nil && *fact.SourceKind == FactSourceRollup {
			t.Fatalf("excluded set still contains rollup: %#v", fact)
		}
	}
	var sawNil bool
	for _, fact := range excluded {
		if fact.SourceKind == nil {
			sawNil = true
		}
	}
	if !sawNil {
		t.Fatalf("ExcludeSourceKind must keep NULL source_kind rows: %#v", excluded)
	}
}
