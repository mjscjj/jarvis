package api

import (
	"context"
	"encoding/json"
	"testing"

	"jarvis/internal/okrworkspace"
	"jarvis/internal/okrworkspace/domain"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCoreObjectiveProjectionRoutes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&domain.Objective{ID: "o-1", Title: "目标", Quarter: "2026-Q3"},
		&domain.KR{ID: "kr-1", ObjectiveID: "o-1", Title: "结果"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	images, err := okrworkspace.NewImageStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	h := server.New()
	if err := RegisterOKRModuleRoutes(h, OKRModuleDependencies{
		Workspace: workspace, Images: images, Enabled: func(context.Context) (bool, error) { return enabled, nil },
	}); err != nil {
		t.Fatal(err)
	}

	response := ut.PerformRequest(h.Engine, "GET", "/api/okr/objectives?quarter=2026-Q3", nil).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("manifest status=%d body=%s", response.StatusCode(), response.Body())
	}
	var manifest struct {
		Data okrworkspace.ObjectiveManifest `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Data.Quarter != "2026-Q3" || manifest.Data.Totals.Objectives != 1 || manifest.Data.Totals.KRs != 1 {
		t.Fatalf("manifest = %+v", manifest.Data)
	}

	response = ut.PerformRequest(h.Engine, "GET", "/api/okr/objectives/o-1", nil).Result()
	if response.StatusCode() != 200 || !json.Valid(response.Body()) {
		t.Fatalf("slice status=%d body=%s", response.StatusCode(), response.Body())
	}
	response = ut.PerformRequest(h.Engine, "GET", "/api/okr/objectives/missing", nil).Result()
	if response.StatusCode() != 404 {
		t.Fatalf("missing status=%d body=%s", response.StatusCode(), response.Body())
	}
	response = ut.PerformRequest(h.Engine, "GET", "/api/okr/objectives?quarter=bad", nil).Result()
	if response.StatusCode() != 400 {
		t.Fatalf("invalid quarter status=%d body=%s", response.StatusCode(), response.Body())
	}
	enabled = false
	response = ut.PerformRequest(h.Engine, "GET", "/api/okr/objectives?quarter=2026-Q3", nil).Result()
	if response.StatusCode() != 404 {
		t.Fatalf("disabled status=%d body=%s", response.StatusCode(), response.Body())
	}
}
