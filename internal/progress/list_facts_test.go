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
	if err := db.Exec(`CREATE TABLE message (id INTEGER PRIMARY KEY AUTOINCREMENT)`).Error; err != nil {
		t.Fatalf("create message table: %v", err)
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

func TestListFactsSourceKindEquality(t *testing.T) {
	t.Parallel()
	service := newFactTestService(t)
	ctx := context.Background()
	day := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	m3 := "m3"
	m5 := "m5"
	insertFact(t, service, "group", 7, "明细 A", day, &m3)
	insertFact(t, service, "group", 7, "明细 NULL source", day.Add(time.Hour), nil)
	insertFact(t, service, "group", 7, "M5 写的明细", day.Add(2*time.Hour), &m5)

	equal, err := service.ListFacts(ctx, FactFilter{
		SubjectType: "group", SubjectID: 7, SourceKind: &m5,
	})
	if err != nil {
		t.Fatalf("ListFacts SourceKind=m5: %v", err)
	}
	if len(equal) != 1 || equal[0].Description != "M5 写的明细" {
		t.Fatalf("SourceKind equality = %#v, want only the m5 fact", equal)
	}

	all, err := service.ListFacts(ctx, FactFilter{SubjectType: "group", SubjectID: 7})
	if err != nil {
		t.Fatalf("ListFacts without source filter: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unfiltered count = %d, want 3 (NULL source_kind must pass): %#v", len(all), all)
	}
}

func TestResourceFactReadsIncludeLegacySubjectType(t *testing.T) {
	service := newFactTestService(t)
	now := time.Now().UTC()
	insertFact(t, service, "managed_resource", 7, "旧类型事实", now, nil)
	insertFact(t, service, "resource", 7, "新类型事实", now.Add(time.Minute), nil)

	items, err := service.ListFacts(t.Context(), FactFilter{SubjectType: "resource", SubjectID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].SubjectType != "resource" || items[1].SubjectType != "resource" {
		t.Fatalf("items = %#v", items)
	}
	count, err := service.CountFacts(t.Context(), FactFilter{SubjectType: "managed_resource", SubjectID: 7})
	if err != nil || count != 2 {
		t.Fatalf("count = %d, error = %v", count, err)
	}
}

func TestAppendFactReturnsExistingExactFactFromSameSourceUnit(t *testing.T) {
	service := newFactTestService(t)
	ctx := context.Background()
	occurredAt := time.Date(2026, 8, 6, 3, 0, 0, 0, time.UTC)
	if err := service.db.Exec(`INSERT INTO message(id) VALUES (336)`).Error; err != nil {
		t.Fatal(err)
	}
	sourceKind := "message"
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
