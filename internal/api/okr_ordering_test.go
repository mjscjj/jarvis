package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"jarvis/internal/okrworkspace"
	"jarvis/internal/okrworkspace/domain"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func orderingTestRouter(t *testing.T, db *gorm.DB) *server.Hertz {
	t.Helper()
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	images, err := okrworkspace.NewImageStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	if err := RegisterOKRModuleRoutes(h, OKRModuleDependencies{
		Workspace: workspace, Images: images,
		Enabled: func(context.Context) (bool, error) { return true, nil },
	}); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestReorderRoutesRewriteSortOrderWithoutTouchingContent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	// Ties and gaps in the stored order are what a swap-two-values move would
	// silently fail on, so the fixture starts with both.
	rows := []any{
		&domain.Objective{ID: "o-1", Quarter: "2026-Q3", Title: "O1", SortOrder: 0},
		&domain.Objective{ID: "o-2", Quarter: "2026-Q3", Title: "O2", SortOrder: 0},
		&domain.Objective{ID: "o-3", Quarter: "2026-Q3", Title: "O3", SortOrder: 9},
		&domain.Objective{ID: "other-quarter", Quarter: "2026-Q2", Title: "别的季度", SortOrder: 0},
		&domain.KR{ID: "kr-a", ObjectiveID: "o-1", Title: "KRA", SortOrder: 0, Version: 3},
		&domain.KR{ID: "kr-b", ObjectiveID: "o-1", Title: "KRB", SortOrder: 1, Version: 4},
		&domain.KR{ID: "kr-elsewhere", ObjectiveID: "o-2", Title: "别的 O 的 KR", SortOrder: 0},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	h := orderingTestRouter(t, db)

	for _, test := range []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{"partial objective order", "/api/okr/objectives/order", `{"quarter":"2026-Q3","objective_ids":["o-3","o-1"]}`, 400},
		{"foreign objective", "/api/okr/objectives/order", `{"quarter":"2026-Q3","objective_ids":["o-1","o-2","other-quarter"]}`, 400},
		{"duplicate objective", "/api/okr/objectives/order", `{"quarter":"2026-Q3","objective_ids":["o-1","o-1","o-2"]}`, 400},
		{"unknown quarter", "/api/okr/objectives/order", `{"quarter":"1999-Q1","objective_ids":[]}`, 404},
		{"kr from another objective", "/api/okr/objectives/o-1/kr-order", `{"kr_ids":["kr-a","kr-elsewhere"]}`, 400},
		{"unknown objective", "/api/okr/objectives/missing/kr-order", `{"kr_ids":["kr-a"]}`, 404},
	} {
		response := ut.PerformRequest(h.Engine, "PUT", test.path, &ut.Body{Body: strings.NewReader(test.body), Len: len(test.body)}).Result()
		if response.StatusCode() != test.status {
			t.Fatalf("%s: status=%d body=%s", test.name, response.StatusCode(), response.Body())
		}
	}

	body := `{"quarter":"2026-Q3","objective_ids":["o-3","o-2","o-1"]}`
	response := ut.PerformRequest(h.Engine, "PUT", "/api/okr/objectives/order", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("objective order status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data struct {
			Order []string `json:"order"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Join(payload.Data.Order, ",") != "o-3,o-2,o-1" {
		t.Fatalf("order = %v", payload.Data.Order)
	}
	var objectives []domain.Objective
	if err := db.Where("quarter = ?", "2026-Q3").Order("sort_order, id").Find(&objectives).Error; err != nil {
		t.Fatal(err)
	}
	if len(objectives) != 3 || objectives[0].ID != "o-3" || objectives[1].ID != "o-2" || objectives[2].ID != "o-1" {
		t.Fatalf("stored objective order = %+v", objectives)
	}
	var untouched domain.Objective
	if err := db.First(&untouched, "id = ?", "other-quarter").Error; err != nil {
		t.Fatal(err)
	}
	if untouched.SortOrder != 0 {
		t.Fatalf("another quarter must keep its order: %+v", untouched)
	}

	krBody := `{"kr_ids":["kr-b","kr-a"]}`
	response = ut.PerformRequest(h.Engine, "PUT", "/api/okr/objectives/o-1/kr-order", &ut.Body{Body: strings.NewReader(krBody), Len: len(krBody)}).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("kr order status=%d body=%s", response.StatusCode(), response.Body())
	}
	var krs []domain.KR
	if err := db.Where("objective_id = ?", "o-1").Order("sort_order, id").Find(&krs).Error; err != nil {
		t.Fatal(err)
	}
	if len(krs) != 2 || krs[0].ID != "kr-b" || krs[1].ID != "kr-a" {
		t.Fatalf("stored kr order = %+v", krs)
	}
	// Moving a row is not a content edit, so open pages elsewhere must keep a
	// usable baseline instead of hitting a spurious conflict on their next save.
	if krs[0].Version != 4 || krs[1].Version != 3 {
		t.Fatalf("reordering must not bump KR versions: %+v", krs)
	}
	if krs[0].Title != "KRB" || krs[1].Title != "KRA" {
		t.Fatalf("reordering must not touch titles: %+v", krs)
	}
}
