package background

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"jarvis/internal/domain"
)

func TestProjectRiskLifecyclePageAndRelation(t *testing.T) {
	db := openBackgroundTestDB(t)
	project := createTestProject(t, db, "Risk project", "active")
	svc, err := NewProjectRiskService(db)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.Create(t.Context(), ProjectRiskInput{
		ProjectID: project.ID, Title: "Capacity shortage", Probability: "high", Impact: "launch delay",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	triggeredAt := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	if _, err := svc.Create(t.Context(), ProjectRiskInput{
		ProjectID: project.ID, Title: "already triggered", TriggeredAt: &triggeredAt,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("create triggered risk error = %v, want ErrInvalidInput", err)
	}
	updated, err := svc.Update(t.Context(), created.ID, ProjectRiskInput{
		ProjectID: project.ID, Title: created.Title, Probability: created.Probability, Impact: created.Impact, TriggeredAt: &triggeredAt,
	})
	if err != nil || updated.TriggeredAt == nil {
		t.Fatalf("Update() = %+v, error = %v", updated, err)
	}
	if _, err := svc.Update(t.Context(), created.ID, ProjectRiskInput{
		ProjectID: project.ID, Title: created.Title, Probability: created.Probability, Impact: created.Impact,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("clear triggered_at error = %v, want ErrInvalidInput", err)
	}

	pages, err := newPageService(t, db).ListPages(t.Context(), ListPagesFilter{Type: PageTypeProjectRisk, PageSize: 20})
	if err != nil || !hasPage(pages, PageTypeProjectRisk, created.ID) {
		t.Fatalf("risk pages = %#v, error = %v", pages, err)
	}
	matter := domain.KeyMatter{Title: "Resolve capacity", ProjectID: &project.ID, LastActiveAt: triggeredAt}
	if err := db.Create(&matter).Error; err != nil {
		t.Fatal(err)
	}
	relations, _ := NewRelationService(db)
	if _, err := relations.Upsert(t.Context(), RelationInput{
		SourceType: PageTypeProjectRisk, SourceID: uintText(created.ID), RelationType: "handled_by",
		TargetType: PageTypeKeyMatter, TargetID: uintText(matter.ID),
	}); err != nil {
		t.Fatalf("risk relation error = %v", err)
	}
	if err := svc.Delete(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(t.Context(), created.ID, ProjectRiskInput{
		ProjectID: project.ID, Title: created.Title, Probability: created.Probability, Impact: created.Impact, TriggeredAt: &triggeredAt,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("update closed risk error = %v, want ErrInvalidInput", err)
	}
	if err := svc.Delete(t.Context(), created.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("second close error = %v", err)
	}
	var factCount int64
	if err := db.Model(&domain.Fact{}).Where("subject_type = ? AND subject_id = ?", PageTypeProjectRisk, created.ID).Count(&factCount).Error; err != nil {
		t.Fatal(err)
	}
	if factCount != 3 {
		t.Fatalf("risk fact count = %d, want create + trigger + close", factCount)
	}
}

func TestProjectChangeLifecycleAndValidation(t *testing.T) {
	db := openBackgroundTestDB(t)
	project := createTestProject(t, db, "Change project", "active")
	svc, err := NewProjectChangeService(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(t.Context(), ProjectChangeInput{ProjectID: project.ID, Title: "missing time"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing changed_at error = %v", err)
	}
	changedAt := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	created, err := svc.Create(t.Context(), ProjectChangeInput{ProjectID: project.ID, Title: "Narrow MVP scope", ChangedAt: changedAt})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	listed, err := svc.List(t.Context(), ProjectChangeFilter{ListFilter: ListFilter{Page: 1, PageSize: 20}, ProjectID: project.ID})
	if err != nil || len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
		t.Fatalf("List() = %+v, error = %v", listed, err)
	}
	if err := svc.Delete(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(t.Context(), created.ID, ProjectChangeInput{ProjectID: project.ID, Title: created.Title, ChangedAt: changedAt}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("update closed change error = %v, want ErrInvalidInput", err)
	}
	var factCount int64
	if err := db.Model(&domain.Fact{}).Where("subject_type = ? AND subject_id = ?", PageTypeProjectChange, created.ID).Count(&factCount).Error; err != nil {
		t.Fatal(err)
	}
	if factCount != 2 {
		t.Fatalf("change fact count = %d, want create + close", factCount)
	}
}

func TestKeyMatterHasNoMatterTypeField(t *testing.T) {
	for _, value := range []any{domain.KeyMatter{}, KeyMatterInput{}, KeyMatterView{}} {
		typ := reflect.TypeOf(value)
		if _, ok := typ.FieldByName("MatterType"); ok {
			t.Fatalf("%s unexpectedly exposes MatterType", typ.Name())
		}
		for index := 0; index < typ.NumField(); index++ {
			if typ.Field(index).Tag.Get("json") == "matter_type" || typ.Field(index).Tag.Get("gorm") == "column:matter_type" {
				t.Fatalf("%s unexpectedly exposes matter_type", typ.Name())
			}
		}
	}
	if err := (&KeyMatterInput{Title: "ordinary matter"}).validate(); err != nil {
		t.Fatalf("ordinary key matter rejected: %v", err)
	}
}
