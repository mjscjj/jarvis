package okrworkspace

import (
	"reflect"
	"testing"

	"jarvis/internal/okrworkspace/domain"
)

func TestObjectiveManifestAndSliceExposeStableDefinitions(t *testing.T) {
	db := openWorkspaceTestDB(t)
	rows := []any{
		&domain.Objective{ID: "o-1", Title: "目标一", Quarter: "2026-Q3", SortOrder: 1},
		&domain.Objective{ID: "o-2", Title: "目标二", Quarter: "2026-Q3", SortOrder: 2},
		&domain.Objective{ID: "o-draft", PlanID: "plan-1", Title: "规划草稿", Quarter: "2026-Q3", SortOrder: 3},
		&domain.Objective{ID: "o-old", Title: "旧目标", Quarter: "2026-Q2"},
		&domain.KR{ID: "kr-1", ObjectiveID: "o-1", Title: "结果一"},
		&domain.KR{ID: "kr-2", ObjectiveID: "o-1", Title: "结果二"},
		&domain.KR{ID: "kr-3", ObjectiveID: "o-2", Title: "结果三"},
		&domain.KR{ID: "kr-draft", ObjectiveID: "o-draft", Title: "草稿结果"},
		&domain.KRMetric{ID: "metric-1", KRID: "kr-1", Text: "指标一"},
		&domain.KRMetric{ID: "metric-2", KRID: "kr-3", Text: "指标二"},
		&domain.KRPoint{ID: "point-1", KRID: "kr-1", Kind: domain.PointKindStrategy, Title: "拆解一", MeegoWorkItemID: "wi-secret", MeegoURL: "https://example.com/wi-secret"},
		&domain.KRPoint{ID: "point-2", KRID: "kr-1", Kind: domain.PointKindProduct, Title: "拆解二"},
		&domain.KRPoint{ID: "point-3", KRID: "kr-3", Kind: domain.PointKindStrategy, Title: "拆解三"},
		&domain.KROwner{KRID: "kr-1", PersonID: 1, OwnerKey: "a", OpenID: "ou_a", Name: "甲"},
		&domain.KROwner{KRID: "kr-1", PersonID: 2, OwnerKey: "b", OpenID: "ou_b", Name: "乙"},
		&domain.KROwner{KRID: "kr-3", PersonID: 1, OwnerKey: "a", OpenID: "ou_a", Name: "甲"},
		&domain.PointOwner{PointID: "point-1", PersonID: 1, OwnerKey: "a", OpenID: "ou_a", Name: "甲"},
		&domain.PointOwner{PointID: "point-3", PersonID: 3, OwnerKey: "c", OpenID: "ou_c", Name: "丙"},
		&domain.KRTag{KRID: "kr-1", Type: domain.TagTypeBusinessCategory, Value: "业务"},
		&domain.PointTag{PointID: "point-1", Type: "custom", Value: "Biz 字段"},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := service.ListObjectiveManifest(t.Context(), "2026-Q3")
	if err != nil {
		t.Fatal(err)
	}
	wantTotals := ObjectiveManifestTotals{Objectives: 2, KRs: 3, Metrics: 2, Points: 3, KROwnerOccurrences: 3, PointOwnerOccurrences: 2}
	if !reflect.DeepEqual(manifest.Totals, wantTotals) {
		t.Fatalf("manifest totals = %+v, want %+v", manifest.Totals, wantTotals)
	}
	wantFirst := ObjectiveManifestItem{ID: "o-1", Title: "目标一", KRCount: 2, MetricCount: 1, PointCount: 2, KROwnerCount: 2, PointOwnerCount: 1}
	if len(manifest.Objectives) != 2 || !reflect.DeepEqual(manifest.Objectives[0], wantFirst) {
		t.Fatalf("manifest objectives = %+v", manifest.Objectives)
	}
	if _, err := service.GetCoreObjective(t.Context(), "o-draft"); err != ErrNotFound {
		t.Fatalf("draft objective error = %v, want ErrNotFound", err)
	}

	slice, err := service.GetCoreObjective(t.Context(), "o-1")
	if err != nil {
		t.Fatal(err)
	}
	if slice.Quarter != "2026-Q3" || slice.Objective.ID != "o-1" || len(slice.Objective.KRs) != 2 {
		t.Fatalf("objective slice = %+v", slice)
	}
	firstKR := slice.Objective.KRs[0]
	if len(firstKR.Owners) != 2 || len(firstKR.Metrics) != 1 || len(firstKR.Points) != 2 {
		t.Fatalf("objective definition incomplete: %+v", firstKR)
	}
	if len(firstKR.Tags) != 0 || len(firstKR.Points[0].Tags) != 0 || firstKR.Points[0].MeegoWorkItemID != "" || len(firstKR.Points[0].Entries) != 0 {
		t.Fatalf("objective slice leaked Biz or progress fields: %+v", firstKR)
	}
}

func TestObjectiveManifestDefaultsLatestAndObjectiveSliceFailsCleanly(t *testing.T) {
	db := openWorkspaceTestDB(t)
	for _, objective := range []domain.Objective{
		{ID: "o-q2", Title: "Q2", Quarter: "2026-Q2"},
		{ID: "o-q3", Title: "Q3", Quarter: "2026-Q3"},
		{ID: "o-invalid", Title: "坏数据", Quarter: "invalid"},
	} {
		if err := db.Create(&objective).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	// Remove the invalid row before asking for the latest scope: ListQuarters is
	// intentionally fail-fast when storage contains an invalid quarter.
	if err := db.Delete(&domain.Objective{}, "id = ?", "o-invalid").Error; err != nil {
		t.Fatal(err)
	}
	manifest, err := service.ListObjectiveManifest(t.Context(), "")
	if err != nil || manifest.Quarter != "2026-Q3" {
		t.Fatalf("latest manifest = %+v, %v", manifest, err)
	}
	if _, err := service.ListObjectiveManifest(t.Context(), "2026-03"); err == nil {
		t.Fatal("invalid quarter was accepted")
	}
	if _, err := service.GetCoreObjective(t.Context(), "missing"); err != ErrNotFound {
		t.Fatalf("missing objective error = %v", err)
	}
	if err := db.Create(&domain.Objective{ID: "o-invalid", Title: "坏数据", Quarter: "invalid"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetCoreObjective(t.Context(), "o-invalid"); err == nil {
		t.Fatal("objective with invalid quarter was accepted")
	}
}
