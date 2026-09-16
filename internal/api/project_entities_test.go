package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestProjectEntityHandlers(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	project := domain.Project{Name: "Project", Role: "owner", Status: "active", Priority: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	risks, _ := background.NewProjectRiskService(db)
	changes, _ := background.NewProjectChangeService(db)
	h := server.New()
	h.GET("/api/project-risks", ListProjectRisks(risks))
	h.POST("/api/project-risks", CreateProjectRisk(risks))
	h.GET("/api/project-changes", ListProjectChanges(changes))
	h.POST("/api/project-changes", CreateProjectChange(changes))

	postJSON := func(path, body string) []byte {
		t.Helper()
		response := ut.PerformRequest(h.Engine, "POST", path, &ut.Body{Body: bytes.NewBufferString(body), Len: len(body)}).Result()
		if response.StatusCode() != consts.StatusOK {
			t.Fatalf("POST %s status=%d body=%s", path, response.StatusCode(), response.Body())
		}
		return response.Body()
	}
	postJSON("/api/project-risks", fmt.Sprintf(`{"project_id":%d,"title":"Risk","probability":"high","impact":"delay","triggered_at":null}`, project.ID))
	postJSON("/api/project-changes", fmt.Sprintf(`{"project_id":%d,"title":"Change","changed_at":"2026-09-16T09:00:00Z"}`, project.ID))
	for _, path := range []string{
		"/api/project-risks?project_id=" + url.QueryEscape(fmt.Sprint(project.ID)) + "&page=1&page_size=20",
		"/api/project-changes?project_id=" + url.QueryEscape(fmt.Sprint(project.ID)) + "&page=1&page_size=20",
	} {
		response := ut.PerformRequest(h.Engine, "GET", path, nil).Result()
		if response.StatusCode() != consts.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.StatusCode(), response.Body())
		}
		var payload struct {
			Data struct {
				Total int               `json:"total"`
				Items []json.RawMessage `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body(), &payload); err != nil || payload.Data.Total != 1 || len(payload.Data.Items) != 1 {
			t.Fatalf("GET %s body=%s error=%v", path, response.Body(), err)
		}
	}
}
