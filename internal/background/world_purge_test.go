package background

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
)

func TestWorldPurgeRemovesClosedGeneratedSetAndMarkedBlocks(t *testing.T) {
	db := openBackgroundTestDB(t)
	service, err := NewWorldPurgeService(db)
	if err != nil {
		t.Fatal(err)
	}
	keep := createTestProject(t, db, "Keep", "active")
	keep.Summary = ptrString("stable before\n\n<!-- generated:q3:start -->\nnoise\n<!-- generated:q3:end -->\n\nstable after")
	if err := db.Save(&keep).Error; err != nil {
		t.Fatal(err)
	}
	drop := createTestProject(t, db, "Drop", "active")
	matter := domain.KeyMatter{Title: "Drop matter", Status: "open", ProjectID: &drop.ID}
	person := domain.Person{OpenID: "ou_drop", Name: "Drop person", Role: "other", PriorityWeight: 0.1, IsActive: true}
	if err := db.Create(&matter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, fact := range []domain.Fact{
		{SubjectType: PageTypeProject, SubjectID: drop.ID, Description: "created", OccurredAt: now},
		{SubjectType: PageTypeKeyMatter, SubjectID: matter.ID, Description: "created", OccurredAt: now},
	} {
		if err := db.Create(&fact).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, revision := range []domain.PageRevision{
		{PageType: PageTypeProject, PageID: drop.ID, OldText: "old", ChangedAt: now},
		{PageType: PageTypeProject, PageID: keep.ID, OldText: "<!-- generated:q3:start -->old<!-- generated:q3:end -->", ChangedAt: now},
	} {
		if err := db.Create(&revision).Error; err != nil {
			t.Fatal(err)
		}
	}
	relations := []domain.EntityRelation{
		{SourceType: "okr_objective", SourceID: "o1", RelationType: "maps_to", TargetType: PageTypeProject, TargetID: uintText(drop.ID), Evidence: datatypes.JSON(`{}`)},
		{SourceType: PageTypeProject, SourceID: uintText(drop.ID), RelationType: "contains", TargetType: PageTypeKeyMatter, TargetID: uintText(matter.ID), Evidence: datatypes.JSON(`{}`)},
		{SourceType: "okr_kr", SourceID: "kr1", RelationType: "owned_by", TargetType: PageTypePerson, TargetID: uintText(person.ID), Evidence: datatypes.JSON(`{}`)},
	}
	if err := db.Create(&relations).Error; err != nil {
		t.Fatal(err)
	}

	result, err := service.Purge(t.Context(), WorldPurgeInput{
		Entities: []WorldPurgeEntity{
			{Type: PageTypeProject, ID: drop.ID, ExpectedName: drop.Name},
			{Type: PageTypeKeyMatter, ID: matter.ID, ExpectedName: matter.Title},
			{Type: PageTypePerson, ID: person.ID, ExpectedName: person.Name},
		},
		PageBlocks: []WorldPurgePageBlock{{Type: PageTypeProject, ID: keep.ID, ExpectedName: keep.Name, Marker: "generated:q3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Entities[PageTypeProject] != 1 || result.Entities[PageTypeKeyMatter] != 1 ||
		result.Entities[PageTypePerson] != 1 || result.Relations != 3 || result.Facts != 2 ||
		result.PageRevisions != 2 || result.PageBlocks != 1 {
		t.Fatalf("result = %#v", result)
	}
	for _, model := range []any{&domain.Project{}, &domain.KeyMatter{}, &domain.Person{}, &domain.EntityRelation{}, &domain.Fact{}, &domain.PageRevision{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if _, ok := model.(*domain.Project); ok {
			if count != 1 {
				t.Fatalf("projects remaining = %d, want keep project only", count)
			}
		} else if count != 0 {
			t.Fatalf("%T remaining = %d", model, count)
		}
	}
	var summary string
	if err := db.Table("project").Select("summary").Where("id = ?", keep.ID).Scan(&summary).Error; err != nil {
		t.Fatal(err)
	}
	if summary != "stable before\n\nstable after" || strings.Contains(summary, "generated:q3") {
		t.Fatalf("cleaned summary = %q", summary)
	}
}

func TestWorldPurgeRejectsPreservedDependencyBeforeDeletingEntities(t *testing.T) {
	db := openBackgroundTestDB(t)
	service, err := NewWorldPurgeService(db)
	if err != nil {
		t.Fatal(err)
	}
	drop := createTestProject(t, db, "Drop", "active")
	keep := createTestProject(t, db, "Keep", "active")
	originalSummary := "stable\n\n<!-- generated:q3:start -->\nnoise\n<!-- generated:q3:end -->\n\nreferences [drop](project:" + uintText(drop.ID) + ")"
	keep.Summary = ptrString(originalSummary)
	if err := db.Save(&keep).Error; err != nil {
		t.Fatal(err)
	}
	_, err = service.Purge(t.Context(), WorldPurgeInput{
		Entities: []WorldPurgeEntity{{Type: PageTypeProject, ID: drop.ID, ExpectedName: drop.Name}},
		PageBlocks: []WorldPurgePageBlock{{
			Type: PageTypeProject, ID: keep.ID, ExpectedName: keep.Name, Marker: "generated:q3",
		}},
	})
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "preserved page") {
		t.Fatalf("error = %v, want preserved-page rejection", err)
	}
	var count int64
	if err := db.Model(&domain.Project{}).Where("id = ?", drop.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("drop project count = %d, error = %v", count, err)
	}
	var summary string
	if err := db.Table("project").Select("summary").Where("id = ?", keep.ID).Scan(&summary).Error; err != nil {
		t.Fatal(err)
	}
	if summary != originalSummary {
		t.Fatalf("preserved page changed before dependency rejection: %q", summary)
	}
}

func TestWorldPurgeAcceptsReferenceRemovedByMarkedBlock(t *testing.T) {
	db := openBackgroundTestDB(t)
	service, err := NewWorldPurgeService(db)
	if err != nil {
		t.Fatal(err)
	}
	drop := createTestProject(t, db, "Drop", "active")
	keep := createTestProject(t, db, "Keep", "active")
	keep.Summary = ptrString("stable\n\n<!-- generated:q3:start -->\n[drop](project:" + uintText(drop.ID) + ")\n<!-- generated:q3:end -->")
	if err := db.Save(&keep).Error; err != nil {
		t.Fatal(err)
	}

	result, err := service.Purge(t.Context(), WorldPurgeInput{
		Entities: []WorldPurgeEntity{{Type: PageTypeProject, ID: drop.ID, ExpectedName: drop.Name}},
		PageBlocks: []WorldPurgePageBlock{{
			Type: PageTypeProject, ID: keep.ID, ExpectedName: keep.Name, Marker: "generated:q3",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Entities[PageTypeProject] != 1 || result.PageBlocks != 1 {
		t.Fatalf("result = %#v", result)
	}
	var summary string
	if err := db.Table("project").Select("summary").Where("id = ?", keep.ID).Scan(&summary).Error; err != nil {
		t.Fatal(err)
	}
	if summary != "stable" {
		t.Fatalf("cleaned summary = %q", summary)
	}
}

func TestWorldPurgeRejectsStaleExpectedName(t *testing.T) {
	db := openBackgroundTestDB(t)
	service, err := NewWorldPurgeService(db)
	if err != nil {
		t.Fatal(err)
	}
	drop := createTestProject(t, db, "Current", "active")
	_, err = service.Purge(t.Context(), WorldPurgeInput{Entities: []WorldPurgeEntity{
		{Type: PageTypeProject, ID: drop.ID, ExpectedName: "Stale"},
	}})
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("error = %v, want expected-name rejection", err)
	}
}

func ptrString(value string) *string { return &value }

func uintText(value uint64) string { return strconv.FormatUint(value, 10) }
