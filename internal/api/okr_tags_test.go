package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"jarvis/internal/okrworkspace"
	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKRTagsRouteUsesNarrowContractAndModuleGate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.KR{ID: "kr-1", Title: "保留标题"}).Error; err != nil {
		t.Fatal(err)
	}
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{}, nil)
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
		DB: db, Workspace: workspace, Images: images, Identity: identity,
		Enabled: func(context.Context) (bool, error) { return enabled, nil },
	}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id     string
		body   string
		status int
	}{
		{"kr-1", `{"expected_version":0,"tags":[],"title":"禁止改标题"}`, 400},
		{"kr-1", `{"expected_version":0,"tags":[],"updated_by":"spoof"}`, 400},
		{"kr-1", `{"expected_version":0}`, 400},
		{"kr-1", `{"expected_version":0,"tags":null}`, 400},
		{"kr-1", `{"expected_version":0,"tags":[{"type":"custom","value":"双周报-SEO"}]}`, 200},
		{"kr-1", `{"expected_version":0,"tags":[]}`, 409},
		{"missing", `{"expected_version":0,"tags":[]}`, 404},
	} {
		response := ut.PerformRequest(h.Engine, "PUT", "/api/okr/krs/"+test.id+"/tags", &ut.Body{Body: strings.NewReader(test.body), Len: len(test.body)}).Result()
		if response.StatusCode() != test.status {
			t.Fatalf("request %s: status=%d body=%s", test.body, response.StatusCode(), response.Body())
		}
		if test.status == 200 || test.status == 409 {
			var payload struct {
				Data okrworkspace.KRView `json:"data"`
			}
			if err := json.Unmarshal(response.Body(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Data.Version != 1 || payload.Data.Title != "保留标题" || len(payload.Data.Tags) != 1 {
				t.Fatalf("response data = %+v", payload.Data)
			}
		}
	}
	var stored domain.KR
	if err := db.First(&stored, "id = ?", "kr-1").Error; err != nil {
		t.Fatal(err)
	}
	if stored.UpdatedBy != "local" {
		t.Fatalf("editor must come from identity: %+v", stored)
	}
	enabled = false
	body := `{"expected_version":1,"tags":[]}`
	response := ut.PerformRequest(h.Engine, "PUT", "/api/okr/krs/kr-1/tags", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != 404 {
		t.Fatalf("disabled module status=%d body=%s", response.StatusCode(), response.Body())
	}
}
