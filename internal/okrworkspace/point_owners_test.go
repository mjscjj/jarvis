package okrworkspace

import (
	"reflect"
	"testing"

	"jarvis/internal/okrworkspace/domain"
)

func TestReplaceKRCorePersistsOwnersPerPoint(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-point-owner", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-point-owner", ObjectiveID: objective.ID, Title: "供给增长"}
	strategy := domain.KRPoint{ID: "point-strategy", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "策略具体KR"}
	product := domain.KRPoint{ID: "point-product", KRID: kr.ID, Kind: domain.PointKindProduct, Title: "产品具体KR"}
	for _, row := range []any{&objective, &kr, &strategy, &product} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.ReplaceKRCore(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: 0,
		Title:           kr.Title,
		Owners:          []OwnerView{{OpenID: "ou_kr", Name: "KR 负责人"}},
		Points: []PointView{
			{ID: strategy.ID, Kind: strategy.Kind, Title: strategy.Title, Tags: []TagView{}, Owners: []OwnerView{
				{OpenID: "ou_a", Name: "甲"},
				{OpenID: "ou_b", Name: "乙"},
				// Duplicates and blanks must be dropped by the shared owner normalizer.
				{OpenID: "OU_A", Name: "甲"},
				{OpenID: "ou_c", Name: "  "},
			}},
			{ID: product.ID, Kind: product.Kind, Title: product.Title, Tags: []TagView{}, Owners: []OwnerView{{OpenID: "ou_c", Name: "丙"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantStrategy := []OwnerView{
		{OpenID: "ou_a", Name: "甲", IdentityNamespace: OwnerIdentityNamespaceMainFeishuApp},
		{OpenID: "ou_b", Name: "乙", IdentityNamespace: OwnerIdentityNamespaceMainFeishuApp},
	}
	wantProduct := []OwnerView{{OpenID: "ou_c", Name: "丙", IdentityNamespace: OwnerIdentityNamespaceMainFeishuApp}}
	if !reflect.DeepEqual(updated.Points[0].Owners, wantStrategy) || !reflect.DeepEqual(updated.Points[1].Owners, wantProduct) {
		t.Fatalf("point owners = %+v / %+v", updated.Points[0].Owners, updated.Points[1].Owners)
	}
	if len(updated.Owners) != 1 || updated.Owners[0].Name != "KR 负责人" {
		t.Fatalf("point owners leaked into the KR level: %+v", updated.Owners)
	}
	reloaded, err := service.GetCoreKR(t.Context(), kr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Points[0].Owners, wantStrategy) || !reflect.DeepEqual(reloaded.Points[1].Owners, wantProduct) {
		t.Fatalf("reloaded point owners = %+v / %+v", reloaded.Points[0].Owners, reloaded.Points[1].Owners)
	}

	// Tag writes are a separate entry point and must not touch owners.
	if _, err := service.ReplacePointTags(t.Context(), strategy.ID, ReplacePointTagsInput{
		ExpectedVersion: reloaded.Version,
		Tags:            []TagView{{Type: "region", Value: "sea"}},
	}); err != nil {
		t.Fatal(err)
	}
	afterTags, err := service.GetCoreKR(t.Context(), kr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterTags.Points[0].Owners, wantStrategy) {
		t.Fatalf("point tag write changed owners: %+v", afterTags.Points[0].Owners)
	}
}

func TestReplaceKRCoreClearsOwnersOfRemovedPoints(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-point-owner-drop", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-point-owner-drop", ObjectiveID: objective.ID, Title: "供给增长"}
	point := domain.KRPoint{ID: "point-dropped", KRID: kr.ID, Kind: domain.PointKindProduct, Title: "会被删掉的具体KR"}
	for _, row := range []any{&objective, &kr, &point} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplaceKRCore(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: 0,
		Title:           kr.Title,
		Points:          []PointView{{ID: point.ID, Kind: point.Kind, Title: point.Title, Tags: []TagView{}, Owners: []OwnerView{{OpenID: "ou_a", Name: "甲"}}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplaceKRCore(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: 1,
		Title:           kr.Title,
		Points:          []PointView{},
	}); err != nil {
		t.Fatal(err)
	}
	var remaining int64
	if err := db.Model(&domain.PointOwner{}).Where("point_id = ?", point.ID).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("removed point kept %d owner rows", remaining)
	}
}

func TestDeleteKRRemovesPointOwners(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-point-owner-delete", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-point-owner-delete", ObjectiveID: objective.ID, Title: "供给增长"}
	point := domain.KRPoint{ID: "point-delete", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "具体KR"}
	for _, row := range []any{
		&objective, &kr, &point,
		&domain.PointOwner{PointID: point.ID, PersonID: 7, OwnerKey: "owner-7", OpenID: "ou_a", Name: "甲"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteKR(t.Context(), kr.ID, DeleteKRInput{ExpectedVersion: 0}); err != nil {
		t.Fatal(err)
	}
	var remaining int64
	if err := db.Model(&domain.PointOwner{}).Where("point_id = ?", point.ID).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("deleted KR kept %d point owner rows", remaining)
	}
}
