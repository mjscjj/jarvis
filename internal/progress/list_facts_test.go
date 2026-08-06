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

func TestAppendFactReturnsExistingExactFactFromSameSourceUnit(t *testing.T) {
	service := newFactTestService(t)
	ctx := context.Background()
	occurredAt := time.Date(2026, 8, 6, 3, 0, 0, 0, time.UTC)
	sourceKind := "task"
	sourceID := uint64(336)
	input := FactInput{
		SubjectType: "meeting", SubjectID: 9, Description: "评审结论已经确认",
		OccurredAt: &occurredAt, SourceKind: &sourceKind, SourceID: &sourceID,
	}
	first, err := service.AppendFact(ctx, input)
	if err != nil {
		t.Fatalf("first AppendFact: %v", err)
	}
	second, err := service.AppendFact(ctx, input)
	if err != nil {
		t.Fatalf("second AppendFact: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("replayed fact ids = %d and %d, want same row", first.ID, second.ID)
	}
	var count int64
	if err := service.db.Model(&domain.Fact{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("fact count = %d, want 1", count)
	}

	changed := input
	changed.Description = "评审结论后来发生变化"
	third, err := service.AppendFact(ctx, changed)
	if err != nil {
		t.Fatalf("changed AppendFact: %v", err)
	}
	if third.ID == first.ID {
		t.Fatal("different fact content from same unit was incorrectly collapsed")
	}
}
