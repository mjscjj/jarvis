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

// The weekly pages send this request while holding point drafts, so the KR
// contract must reject points and leave every concrete KR untouched.
func TestKRDefinitionRouteNeverWritesPoints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	rows := []any{
		&domain.KR{ID: "kr-1", Title: "旧 KR 标题", MetricNote: "主干指标说明"},
		&domain.KRMetric{ID: "metric-1", KRID: "kr-1", Text: "主干核心数据", Light: domain.LightGreen},
		&domain.KRPoint{ID: "point-1", KRID: "kr-1", Kind: domain.PointKindStrategy, Title: "旧要点标题", SortOrder: 0},
		&domain.KRPoint{ID: "point-2", KRID: "kr-1", Kind: domain.PointKindStrategy, Title: "第二个要点", SortOrder: 1},
		&domain.KROwner{KRID: "kr-1", OwnerKey: "old", Name: "旧负责人"},
	}
	for _, row := range rows {
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
		Workspace: workspace, Images: images,
		Enabled: func(context.Context) (bool, error) { return enabled, nil },
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		id     string
		body   string
		status int
	}{
		{"metrics are not part of the contract", "kr-1", `{"expected_version":0,"title":"新标题","metrics":[]}`, 400},
		{"labels are not part of the contract", "kr-1", `{"expected_version":0,"title":"新标题","tags":[]}`, 400},
		{"editor cannot be spoofed", "kr-1", `{"expected_version":0,"title":"新标题","updated_by":"spoof"}`, 400},
		{"title stays required", "kr-1", `{"expected_version":0,"title":"  "}`, 400},
		{"points cannot be added", "kr-1", `{"expected_version":0,"title":"新标题","points":[{"id":"point-new","title":"凭空新增"}]}`, 400},
		{"missing kr", "missing", `{"expected_version":0,"title":"新标题"}`, 404},
	} {
		response := ut.PerformRequest(h.Engine, "PUT", "/api/okr/krs/"+test.id+"/definition", &ut.Body{Body: strings.NewReader(test.body), Len: len(test.body)}).Result()
		if response.StatusCode() != test.status {
			t.Fatalf("%s: status=%d body=%s", test.name, response.StatusCode(), response.Body())
		}
	}

	body := `{"expected_version":0,"title":"新 KR 标题","owners":[{"open_id":"ou_new","name":"新负责人"}]}`
	response := ut.PerformRequest(h.Engine, "PUT", "/api/okr/krs/kr-1/definition", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("definition status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data okrworkspace.KRView `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	view := payload.Data
	if view.Title != "新 KR 标题" || view.OwnerName != "新负责人" || view.Version != 1 {
		t.Fatalf("wording and people were not applied: %+v", view)
	}
	if len(view.Points) != 2 || view.Points[0].ID != "point-1" || view.Points[0].Title != "旧要点标题" || len(view.Points[0].Owners) != 0 {
		t.Fatalf("KR definition write touched points: %+v", view.Points)
	}
	if view.MetricNote != "主干指标说明" || len(view.Metrics) != 1 || view.Metrics[0].Text != "主干核心数据" {
		t.Fatalf("definition metrics must survive a wording edit: %+v", view)
	}
	if len(view.Tags) != 0 {
		t.Fatalf("generic OKR response must not expose Biz tags: %+v", view.Tags)
	}
	var stored domain.KR
	if err := db.First(&stored, "id = ?", "kr-1").Error; err != nil {
		t.Fatal(err)
	}
	if stored.UpdatedBy != "jarvis" {
		t.Fatalf("editor must come from identity: %+v", stored)
	}

	stale := `{"expected_version":0,"title":"再改一次"}`
	response = ut.PerformRequest(h.Engine, "PUT", "/api/okr/krs/kr-1/definition", &ut.Body{Body: strings.NewReader(stale), Len: len(stale)}).Result()
	if response.StatusCode() != 409 {
		t.Fatalf("stale version status=%d body=%s", response.StatusCode(), response.Body())
	}

	enabled = false
	current := `{"expected_version":1,"title":"模块关闭后不该生效"}`
	response = ut.PerformRequest(h.Engine, "PUT", "/api/okr/krs/kr-1/definition", &ut.Body{Body: strings.NewReader(current), Len: len(current)}).Result()
	if response.StatusCode() != 404 {
		t.Fatalf("disabled module status=%d body=%s", response.StatusCode(), response.Body())
	}
}

func TestPointDefinitionRoutePatchesOnlyOneExistingPoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&domain.Objective{ID: "o-1", Quarter: "2026-Q3", Title: "O"},
		&domain.KR{ID: "kr-1", ObjectiveID: "o-1", Title: "KR"},
		&domain.KRPoint{ID: "strategy", KRID: "kr-1", Kind: domain.PointKindStrategy, Title: "旧策略"},
		&domain.KRPoint{ID: "product", KRID: "kr-1", Kind: domain.PointKindProduct, Title: "旧产品"},
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
	h := server.New()
	if err := RegisterOKRModuleRoutes(h, OKRModuleDependencies{
		Workspace: workspace, Images: images,
		Enabled: func(context.Context) (bool, error) { return true, nil },
	}); err != nil {
		t.Fatal(err)
	}

	for _, request := range []struct {
		id, body string
	}{
		{"strategy", `{"expected_version":0,"title":"新策略"}`},
		{"product", `{"expected_version":0,"title":"新产品","owners":[{"open_id":"ou_owner","name":"负责人"}]}`},
	} {
		response := ut.PerformRequest(h.Engine, "PATCH", "/api/okr/points/"+request.id+"/definition", &ut.Body{Body: strings.NewReader(request.body), Len: len(request.body)}).Result()
		if response.StatusCode() != 200 {
			t.Fatalf("patch %s status=%d body=%s", request.id, response.StatusCode(), response.Body())
		}
	}
	view, err := workspace.GetCoreKR(t.Context(), "kr-1")
	if err != nil {
		t.Fatal(err)
	}
	points := make(map[string]okrworkspace.PointView, len(view.Points))
	for _, point := range view.Points {
		points[point.ID] = point
	}
	if view.Version != 0 || points["strategy"].Version != 1 || points["product"].Version != 1 || points["strategy"].Title != "新策略" || points["product"].Title != "新产品" || len(points["product"].Owners) != 1 {
		t.Fatalf("point patches = %+v", view)
	}
	staleBody := `{"expected_version":0,"title":"旧页面覆盖"}`
	staleResponse := ut.PerformRequest(h.Engine, "PATCH", "/api/okr/points/strategy/definition", &ut.Body{Body: strings.NewReader(staleBody), Len: len(staleBody)}).Result()
	if staleResponse.StatusCode() != 409 {
		t.Fatalf("stale point patch status=%d body=%s", staleResponse.StatusCode(), staleResponse.Body())
	}
	var conflict struct {
		Data okrworkspace.PointDefinitionPatchResult `json:"data"`
	}
	if err := json.Unmarshal(staleResponse.Body(), &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.Data.Version != 1 || conflict.Data.Title != "新策略" {
		t.Fatalf("stale point conflict = %+v", conflict.Data)
	}

	for _, body := range []string{`{}`, `{"title":" "}`, `{"owners":[],"extra":true}`} {
		response := ut.PerformRequest(h.Engine, "PATCH", "/api/okr/points/strategy/definition", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
		if response.StatusCode() != 400 {
			t.Fatalf("invalid body %s status=%d body=%s", body, response.StatusCode(), response.Body())
		}
	}
}

func TestGenericKRRouteCanMaintainDecompositionWithoutBizSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	objective := domain.Objective{ID: "o-generic", Quarter: "2026-Q3", Title: "通用目标"}
	kr := domain.KR{ID: "kr-generic", ObjectiveID: objective.ID, Title: "待拆解 KR"}
	for _, row := range []any{&objective, &kr} {
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
	h := server.New()
	if err := RegisterOKRModuleRoutes(h, OKRModuleDependencies{
		Workspace: workspace, Images: images,
		Enabled: func(context.Context) (bool, error) { return true, nil },
	}); err != nil {
		t.Fatal(err)
	}

	body := `{"expected_version":0,"title":"已拆解 KR","metric_note":"季度口径","metrics":[{"id":"metric-1","text":"完成率 100%","light":"green","images":[]}],"points":[{"id":"point-1","kind":"strategy","title":"通用拆解点","owners":[{"open_id":"ou_owner","name":"负责人"}]}],"owners":[]}`
	response := ut.PerformRequest(h.Engine, "PUT", "/api/okr/krs/kr-generic", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("generic replace status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data okrworkspace.KRView `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Title != "已拆解 KR" || len(payload.Data.Metrics) != 1 || len(payload.Data.Points) != 1 || len(payload.Data.Points[0].Owners) != 1 {
		t.Fatalf("generic KR = %+v", payload.Data)
	}
	if payload.Data.Tags == nil || len(payload.Data.Tags) != 0 || payload.Data.Points[0].Tags == nil || payload.Data.Points[0].MeegoWorkItemID != "" {
		t.Fatalf("generic KR leaked Biz data: %+v", payload.Data)
	}
	if payload.Data.DeleteToken == "" {
		t.Fatal("generic KR response is missing its structure deletion snapshot")
	}
	pointPatch := `{"expected_version":0,"title":"协作者刚保存的 Point"}`
	response = ut.PerformRequest(h.Engine, "PATCH", "/api/okr/points/point-1/definition", &ut.Body{Body: strings.NewReader(pointPatch), Len: len(pointPatch)}).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("collaborator point patch status=%d body=%s", response.StatusCode(), response.Body())
	}
	staleRemoval, err := json.Marshal(map[string]any{
		"expected_version": payload.Data.Version,
		"delete_token":     payload.Data.DeleteToken,
		"title":            payload.Data.Title,
		"metric_note":      payload.Data.MetricNote,
		"metrics":          payload.Data.Metrics,
		"points":           []any{},
		"owners":           payload.Data.Owners,
	})
	if err != nil {
		t.Fatal(err)
	}
	response = ut.PerformRequest(h.Engine, "PUT", "/api/okr/krs/kr-generic", &ut.Body{Body: strings.NewReader(string(staleRemoval)), Len: len(staleRemoval)}).Result()
	if response.StatusCode() != 409 {
		t.Fatalf("stale point removal status=%d body=%s", response.StatusCode(), response.Body())
	}
	current, err := workspace.GetCoreKR(t.Context(), "kr-generic")
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Points) != 1 || current.Points[0].Title != "协作者刚保存的 Point" {
		t.Fatalf("stale structural save removed collaborator point: %+v", current.Points)
	}

	for _, forbidden := range []string{
		`{"expected_version":1,"title":"越界","metric_note":"","metrics":[],"points":[],"owners":[],"tags":[]}`,
		`{"expected_version":1,"title":"越界","metric_note":"","metrics":[],"points":[{"id":"point-1","kind":"strategy","title":"拆解","owners":[],"meego_url":"https://example.com"}],"owners":[]}`,
	} {
		response = ut.PerformRequest(h.Engine, "PUT", "/api/okr/krs/kr-generic", &ut.Body{Body: strings.NewReader(forbidden), Len: len(forbidden)}).Result()
		if response.StatusCode() != 400 {
			t.Fatalf("generic route accepted Biz field: status=%d body=%s", response.StatusCode(), response.Body())
		}
	}
}
