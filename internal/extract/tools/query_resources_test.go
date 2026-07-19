package tools

import (
	"context"
	"encoding/json"
	"os"
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

func TestQueryResourcesRejectsBadArgs(t *testing.T) {
	// db is only touched after argument validation, so a tool without a db still
	// exercises the validation branches deterministically.
	tool := &QueryResourcesTool{timeout: time.Second, maxLimit: 20}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"project_id":null,"person_open_id":null,"principal_only":null,"keyword":null,"limit":0}`)); err == nil {
		t.Fatal("Invoke accepted non-positive limit")
	}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"project_id":null,"person_open_id":null,"principal_only":null,"keyword":null,"limit":10,"extra":1}`)); err == nil {
		t.Fatal("Invoke accepted unknown argument field")
	}
}

// TestQueryResourcesMySQL validates project/person/principal/keyword filtering,
// the active-only guard and the limit cap against MySQL. Requires an empty
// database in JARVIS_TOOLS_TEST_MYSQL_DSN.
func TestQueryResourcesMySQL(t *testing.T) {
	dsn := os.Getenv("JARVIS_TOOLS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JARVIS_TOOLS_TEST_MYSQL_DSN is required for query_resources integration test")
	}
	db, err := store.OpenMySQL(context.Background(), config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: 60,
	})
	if err != nil {
		t.Fatalf("OpenMySQL() error = %v", err)
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
	tool := newResourcesTool(t, tx, 20)

	// By project: only the active project-linked doc, not the disabled one.
	out := invokeResources(t, tool, `{"project_id":`+u64(project.ID)+`,"person_open_id":null,"principal_only":null,"keyword":null,"limit":10}`)
	if out.Count != 1 || out.Resources[0].Title != "项目方案" {
		t.Fatalf("project filter result = %#v", out)
	}

	// By person open_id.
	out = invokeResources(t, tool, `{"project_id":null,"person_open_id":"ou_res_alice","principal_only":null,"keyword":null,"limit":10}`)
	if out.Count != 1 || out.Resources[0].Title != "Alice 的仓库" {
		t.Fatalf("person filter result = %#v", out)
	}

	// Principal only.
	out = invokeResources(t, tool, `{"project_id":null,"person_open_id":null,"principal_only":true,"keyword":null,"limit":10}`)
	if out.Count != 1 || out.Resources[0].Title != "我的偏好清单" {
		t.Fatalf("principal filter result = %#v", out)
	}

	// Keyword matches description.
	out = invokeResources(t, tool, `{"project_id":null,"person_open_id":null,"principal_only":null,"keyword":"runtime","limit":10}`)
	if out.Count != 1 || out.Resources[0].Title != "项目方案" {
		t.Fatalf("keyword filter result = %#v", out)
	}

	// Unknown person open_id yields empty, not an error.
	out = invokeResources(t, tool, `{"project_id":null,"person_open_id":"ou_missing","principal_only":null,"keyword":null,"limit":10}`)
	if out.Count != 0 {
		t.Fatalf("missing person result = %#v", out)
	}

	// Limit cap: only active resources counted, capped to 1.
	capTool := newResourcesTool(t, tx, 1)
	out = invokeResources(t, capTool, `{"project_id":null,"person_open_id":null,"principal_only":null,"keyword":null,"limit":10}`)
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

func u64(v uint64) string {
	return strconv.FormatUint(v, 10)
}
