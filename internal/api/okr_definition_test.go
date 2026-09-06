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

// The weekly pages send this request while holding week-scoped metric values,
// so the contract has to make wording and people the only reachable fields.
func TestKRDefinitionRouteEditsOnlyWordingAndPeople(t *testing.T) {
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
		&domain.KRPoint{ID: "point-1", KRID: "kr-1", Kind: domain.PointKindStrategy, Title: "旧要点标题"},
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

	body := `{"expected_version":0,"title":"新 KR 标题","owners":[{"open_id":"ou_new","name":"新负责人"}],"points":[{"id":"point-1","title":"新要点标题","owners":[{"open_id":"ou_point","name":"要点负责人"}]}]}`
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
	if len(view.Points) != 1 || view.Points[0].Title != "新要点标题" || len(view.Points[0].Owners) != 1 || view.Points[0].Owners[0].Name != "要点负责人" {
		t.Fatalf("point wording and people were not applied: %+v", view.Points)
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
