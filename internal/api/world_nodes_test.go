package api

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/okrworkspace"
	okrdomain "jarvis/internal/okrworkspace/domain"
	"jarvis/internal/store"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResolveWorldNodeReadsEachAuthoritativeModule(t *testing.T) {
	worldDB, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(worldDB) })
	if err := store.Migrate(worldDB); err != nil {
		t.Fatal(err)
	}
	project := domain.Project{Name: "现实项目", Role: "owner", Status: "active", Priority: 1}
	if err := worldDB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	pages, err := background.NewPageService(worldDB)
	if err != nil {
		t.Fatal(err)
	}

	okrDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.Migrate(okrDB); err != nil {
		t.Fatal(err)
	}
	rows := []any{
		&okrdomain.Objective{ID: "o-1", Quarter: "2026-Q3", Title: "正式目标"},
		&okrdomain.KR{ID: "kr-1", ObjectiveID: "o-1", Title: "正式 KR"},
		&okrdomain.KRPoint{ID: "point-1", KRID: "kr-1", Kind: okrdomain.PointKindStrategy, Title: "正式 Point"},
		&okrdomain.Objective{ID: "plan-o-1", PlanID: "plan-1", Quarter: "2026-Q4", Title: "规划目标"},
		&okrdomain.KR{ID: "plan-kr-1", ObjectiveID: "plan-o-1", Title: "规划 KR"},
		&okrdomain.KRPoint{ID: "plan-point-1", KRID: "plan-kr-1", Kind: okrdomain.PointKindProduct, Title: "规划 Point"},
	}
	now := time.Now().UTC()
	plan := map[string]any{
		"id": "plan-1", "quarter": "2026-Q4", "title": "业务规划",
		"created_at": now, "updated_at": now,
	}
	if okrDB.Migrator().HasColumn(&okrdomain.OKRPlan{}, "content") {
		plan["content"] = []byte(`{"objectives":[]}`)
	}
	if err := okrDB.Table(okrdomain.OKRPlan{}.TableName()).Create(plan).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if err := okrDB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := okrworkspace.NewService(okrDB)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	enabled := func(context.Context) (bool, error) { return true, nil }
	h.GET("/api/world-nodes/:type/:id", ResolveWorldNode(
		pages,
		&OKRModuleDependencies{Workspace: workspace, Enabled: enabled},
		&BizOKRModuleDependencies{Workspace: workspace, Enabled: enabled},
	))

	for _, test := range []struct {
		path     string
		wantType string
		wantID   string
		wantName string
	}{
		{fmt.Sprintf("/api/world-nodes/project/%d", project.ID), "project", fmt.Sprint(project.ID), "现实项目"},
		{"/api/world-nodes/okr_objective/o-1", "okr_objective", "o-1", "正式目标"},
		{"/api/world-nodes/okr_kr/kr-1", "okr_kr", "kr-1", "正式 KR"},
		{"/api/world-nodes/okr_point/point-1", "okr_point", "point-1", "正式 Point"},
		{"/api/world-nodes/biz_okr_plan_objective/plan-o-1", "biz_okr_plan_objective", "plan-o-1", "规划目标"},
		{"/api/world-nodes/biz_okr_plan_kr/plan-kr-1", "biz_okr_plan_kr", "plan-kr-1", "规划 KR"},
		{"/api/world-nodes/biz_okr_plan_point/plan-point-1", "biz_okr_plan_point", "plan-point-1", "规划 Point"},
	} {
		response := ut.PerformRequest(h.Engine, "GET", test.path, nil).Result()
		if response.StatusCode() != 200 {
			t.Fatalf("GET %s: status=%d body=%s", test.path, response.StatusCode(), response.Body())
		}
		var payload struct {
			Data worldNodeView `json:"data"`
		}
		if err := json.Unmarshal(response.Body(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Data.Type != test.wantType || payload.Data.ID != test.wantID || payload.Data.Name != test.wantName || payload.Data.Data == nil {
			t.Fatalf("GET %s: data=%+v", test.path, payload.Data)
		}
	}
}

func TestResolveWorldNodeRejectsUnknownType(t *testing.T) {
	h := server.New()
	h.GET("/api/world-nodes/:type/:id", ResolveWorldNode(nil, nil, nil))
	response := ut.PerformRequest(h.Engine, "GET", "/api/world-nodes/unknown/id-1", nil).Result()
	if response.StatusCode() != 400 {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}
