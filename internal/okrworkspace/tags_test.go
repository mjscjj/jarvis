package okrworkspace

import (
	"errors"
	"reflect"
	"testing"

	"jarvis/internal/okrworkspace/domain"
)

func TestReplaceKRTagsPreservesDefinitionsAndWeeklyFacts(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-tags", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-tags", ObjectiveID: objective.ID, Title: "SEO", MetricNote: "季度指标"}
	point := domain.KRPoint{ID: "point-tags", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "官网", MeegoWorkItemID: "wi-1"}
	progress := domain.KRProgress{ID: "progress-tags", PointID: point.ID, Week: "2026-W35", Status: domain.StatusDone, Text: "已发布"}
	for _, row := range []any{
		&objective, &kr, &point, &progress,
		&domain.KRMetric{ID: "metric-tags", KRID: kr.ID, Text: "流量 100", Light: domain.LightGreen},
		&domain.KROwner{KRID: kr.ID, OwnerKey: "ou_owner", OpenID: "ou_owner", Name: "负责人"},
		&domain.KRTag{KRID: kr.ID, Type: "region", Value: "eu"},
		&domain.WeeklyReportWeek{Quarter: objective.Quarter, Week: progress.Week, OpenedBy: "test"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.GetKR(t.Context(), kr.ID, progress.Week)
	if err != nil {
		t.Fatal(err)
	}
	tags := []TagView{{Type: "custom", Value: "双周报-官网SEO"}, {Type: "region", Value: "eu"}}
	updated, err := service.ReplaceKRTags(t.Context(), kr.ID, ReplaceKRTagsInput{ExpectedVersion: before.Version, Tags: tags, UpdatedBy: "ou_editor"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != before.Version+1 || !reflect.DeepEqual(updated.Tags, tags) {
		t.Fatalf("updated version/tags = %d / %+v", updated.Version, updated.Tags)
	}
	if _, err := service.ReplaceKRTags(t.Context(), kr.ID, ReplaceKRTagsInput{ExpectedVersion: before.Version, Tags: []TagView{}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write error = %v", err)
	}
	after, err := service.GetKR(t.Context(), kr.ID, progress.Week)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Tags, tags) {
		t.Fatalf("stale write changed tags: %+v", after.Tags)
	}
	after.Tags, after.Version = before.Tags, before.Version
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("tag write changed definition or weekly facts:\nbefore=%+v\nafter=%+v", before, after)
	}
	var stored domain.KR
	if err := db.First(&stored, "id = ?", kr.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.UpdatedBy != "ou_editor" || stored.ObjectiveID != objective.ID {
		t.Fatalf("stored KR = %+v", stored)
	}
	cleared, err := service.ReplaceKRTags(t.Context(), kr.ID, ReplaceKRTagsInput{ExpectedVersion: updated.Version, Tags: []TagView{}, UpdatedBy: "ou_editor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Tags) != 0 || cleared.Version != updated.Version+1 {
		t.Fatalf("clear result = %+v", cleared)
	}
}

func TestReplaceKRTagsValidatesBeforeWriting(t *testing.T) {
	db := openWorkspaceTestDB(t)
	kr := domain.KR{ID: "kr-validate-tags", Title: "保持"}
	if err := db.Create(&kr).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []ReplaceKRTagsInput{
		{Tags: nil},
		{ExpectedVersion: -1, Tags: []TagView{}},
		{Tags: []TagView{{Type: "custom", Value: " "}}},
		{Tags: []TagView{{Type: "custom", Value: "a"}, {Type: "custom", Value: " a "}}},
		{Tags: []TagView{{Type: "priority", Value: "p3"}}},
		{Tags: []TagView{{Type: "priority", Value: "p0"}, {Type: "priority", Value: "p1"}}},
		{Tags: []TagView{{Type: "business_category", Value: "a"}, {Type: "business_category", Value: "b"}}},
	} {
		if _, err := service.ReplaceKRTags(t.Context(), kr.ID, input); err == nil {
			t.Fatalf("accepted invalid tags: %+v", input)
		}
	}
	if _, err := service.ReplaceKRTags(t.Context(), "missing", ReplaceKRTagsInput{Tags: []TagView{}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing KR error = %v", err)
	}
	current, err := service.GetCoreKR(t.Context(), kr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 0 || len(current.Tags) != 0 {
		t.Fatalf("invalid writes changed KR: %+v", current)
	}
	valid, err := service.ReplaceKRTags(t.Context(), kr.ID, ReplaceKRTagsInput{Tags: []TagView{
		{Type: " priority ", Value: " p0 "}, {Type: "business_category", Value: "agency"}, {Type: "future_type", Value: "自由标签"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(valid.Tags) != 3 || valid.Tags[2] != (TagView{Type: "priority", Value: "p0"}) {
		t.Fatalf("normalized tags = %+v", valid.Tags)
	}
}
