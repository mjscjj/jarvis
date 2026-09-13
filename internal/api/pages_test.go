package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestAgentIdentityUsesConfiguredPrincipalWithoutSavedProfile(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	profile, err := background.NewProfileService(db, "ou_configured")
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.GET("/api/agent-identity", GetAgentIdentity("Friday", profile))
	response := ut.PerformRequest(h.Engine, "GET", "/api/agent-identity", nil).Result()
	var result struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode() != 200 || len(result.Data) != 2 || result.Data["display_name"] != "Friday" || result.Data["principal_open_id"] != "ou_configured" {
		t.Fatalf("identity response = %s", response.Body())
	}
}

func TestUpdateProjectRejectsSummaryField(t *testing.T) {
	h := server.New()
	h.PUT("/api/projects/:project_id", UpdateProject(nil))
	body := []byte(`{"name":"Jarvis","role":"owner","status":"active","priority":1,"summary":"不应从这里写"}`)
	response := ut.PerformRequest(h.Engine, "PUT", "/api/projects/1", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestUpdatePageConflictReturns409WithCurrentContent(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	svc, err := background.NewPageService(db)
	if err != nil {
		t.Fatalf("NewPageService() error = %v", err)
	}
	project := domain.Project{Name: "CAS", Role: "owner", Status: "active", Priority: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	page, err := svc.GetPage(t.Context(), background.PageTypeProject, project.ID)
	if err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}
	written, err := svc.UpdatePage(t.Context(), background.PageTypeProject, project.ID, background.UpdatePageInput{
		Content: "线上当前全文", IfUnchangedSince: page.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("seed UpdatePage() error = %v", err)
	}

	h := server.New()
	h.PUT("/api/pages/:type/:id", UpdatePage(svc))
	stale := page.UpdatedAt.UTC().Format(time.RFC3339)
	body := []byte(fmt.Sprintf(`{"content":"过期草稿","if_unchanged_since":%q}`, stale))
	response := ut.PerformRequest(
		h.Engine, "PUT", fmt.Sprintf("/api/pages/project/%d", project.ID),
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
	).Result()
	if response.StatusCode() != consts.StatusConflict {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Code int                 `json:"code"`
		Data background.PageView `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 40924 {
		t.Fatalf("code = %d, want 40924 body=%s", payload.Code, response.Body())
	}
	if payload.Data.Summary != "线上当前全文" {
		t.Fatalf("current summary = %q, want 线上当前全文; written=%#v", payload.Data.Summary, written)
	}
}
