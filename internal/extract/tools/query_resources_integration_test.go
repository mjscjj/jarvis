//go:build integration

package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"

	"gorm.io/gorm"
)

func newResourcesTool(t *testing.T, db *gorm.DB, maxLimit int) *QueryResourcesTool {
	t.Helper()
	tool, err := NewQueryResourcesTool(db, time.Second, maxLimit)
	if err != nil {
		t.Fatalf("NewQueryResourcesTool() error = %v", err)
	}
	return tool
}

func TestQueryResourcesSQLite(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "jarvis.db"),
	})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	tx := db.Begin()
	t.Cleanup(func() { tx.Rollback() })

	person := domain.Person{OpenID: "ou_res_alice", Name: "Alice", Role: "colleague", PriorityWeight: 0.4, IsActive: true}
	if err := tx.Create(&person).Error; err != nil {
		t.Fatalf("seed person: %v", err)
	}
	project := domain.Project{Name: "Infra", Role: "participant", Status: "active", Priority: 3}
	if err := tx.Create(&project).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	docURL := "https://example.com/doc"
	desc := "runtime 方案要点"
	seed := []domain.ManagedResource{
		{Title: "项目方案", ResourceType: "doc", URL: &docURL, Description: &desc, ProjectID: &project.ID, IsActive: true},
		{Title: "Alice 的仓库", ResourceType: "repo", PersonID: &person.ID, IsActive: true},
		{Title: "我的偏好清单", ResourceType: "note", LinkPrincipal: true, IsActive: true},
		{Title: "停用资源", ResourceType: "link", ProjectID: &project.ID, IsActive: false},
	}
	if err := tx.Create(&seed).Error; err != nil {
		t.Fatalf("seed resources: %v", err)
	}
	if err := tx.Model(&seed[3]).Update("is_active", false).Error; err != nil {
		t.Fatalf("disable fixture resource: %v", err)
	}
	tool := newResourcesTool(t, tx, 20)

	out := invokeResources(t, tool, `{"project_id":`+strconv.FormatUint(project.ID, 10)+`,"person_open_id":null,"principal_only":null,"keyword":null,"limit":10}`)
	if out.Count != 1 || out.Resources[0].Title != "项目方案" {
		t.Fatalf("project filter result = %#v", out)
	}
	out = invokeResources(t, tool, `{"project_id":null,"person_open_id":"ou_res_alice","principal_only":null,"keyword":null,"limit":10}`)
	if out.Count != 1 || out.Resources[0].Title != "Alice 的仓库" {
		t.Fatalf("person filter result = %#v", out)
	}
	out = invokeResources(t, tool, `{"project_id":null,"person_open_id":null,"principal_only":true,"keyword":null,"limit":10}`)
	if out.Count != 1 || out.Resources[0].Title != "我的偏好清单" {
		t.Fatalf("principal filter result = %#v", out)
	}
	out = invokeResources(t, tool, `{"project_id":null,"person_open_id":null,"principal_only":null,"keyword":"runtime","limit":10}`)
	if out.Count != 1 || out.Resources[0].Title != "项目方案" {
		t.Fatalf("keyword filter result = %#v", out)
	}
	out = invokeResources(t, tool, `{"project_id":null,"person_open_id":"ou_missing","principal_only":null,"keyword":null,"limit":10}`)
	if out.Count != 0 {
		t.Fatalf("missing person result = %#v", out)
	}
	out = invokeResources(t, newResourcesTool(t, tx, 1), `{"project_id":null,"person_open_id":null,"principal_only":null,"keyword":null,"limit":10}`)
	if out.Count != 1 {
		t.Fatalf("limit cap result = %#v", out)
	}
}

func invokeResources(t *testing.T, tool *QueryResourcesTool, args string) queryResourcesResult {
	t.Helper()
	raw, err := tool.Invoke(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("Invoke(%s) error = %v", args, err)
	}
	var result queryResourcesResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return result
}
