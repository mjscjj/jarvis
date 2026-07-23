package contextsnap

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAssemblerLoadsCommonContextAndPreservesRequestContext(t *testing.T) {
	db := openAssemblerTestDB(t)
	description := "Jarvis 个人助手"
	repoURL := "https://example.com/jarvis"
	project := domain.Project{
		Name: "Jarvis", Role: "owner", Status: "active", Priority: 1,
		Description: &description, Repos: []byte(`[{"path":"/workspace/jarvis"}]`),
		TechStack: []byte(`["Go"]`), KeyDecisions: []byte(`["agent-first"]`),
		Timeline: []byte(`{"mvp":"2026-07"}`),
	}
	other := domain.Project{Name: "Other", Role: "participant", Status: "active", Priority: 3}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("create other project: %v", err)
	}
	if err := db.Create(&domain.PrincipalProfile{OpenID: "ou_me", Name: "我"}).Error; err != nil {
		t.Fatalf("create principal: %v", err)
	}
	if err := db.Create(&domain.ManagedResource{
		Title: "Jarvis repo", ResourceType: "repo", URL: &repoURL,
		ProjectID: &project.ID, IsActive: true,
	}).Error; err != nil {
		t.Fatalf("create managed resource: %v", err)
	}
	eventAt := time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC)
	if err := db.Create(&domain.ProjectEvent{
		ProjectID: project.ID, Description: "完成上下文链路", OccurredAt: eventAt,
	}).Error; err != nil {
		t.Fatalf("create project event: %v", err)
	}

	assembler, err := NewAssembler(db, "ou_me")
	if err != nil {
		t.Fatalf("NewAssembler() error = %v", err)
	}
	raw, err := assembler.Assemble(t.Context(), AssembleOptions{
		ProjectID: &project.ID, RequestContext: json.RawMessage(`{"instruction_context":"只改后端"}`),
	})
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	snapshot, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if snapshot.Principal == nil || snapshot.Principal.OpenID != "ou_me" {
		t.Fatalf("principal = %#v", snapshot.Principal)
	}
	if snapshot.Project == nil || snapshot.Project.ID != project.ID || string(snapshot.Project.TechStack) != `["Go"]` {
		t.Fatalf("project = %#v", snapshot.Project)
	}
	if len(snapshot.OtherProjects) != 1 || snapshot.OtherProjects[0].ID != other.ID {
		t.Fatalf("other_projects = %#v", snapshot.OtherProjects)
	}
	if len(snapshot.ManagedResources) != 1 || len(snapshot.ProjectEvents) != 1 {
		t.Fatalf("resources/events = %#v / %#v", snapshot.ManagedResources, snapshot.ProjectEvents)
	}
	if string(snapshot.RequestContext) != `{"instruction_context":"只改后端"}` {
		t.Fatalf("request_context = %s", snapshot.RequestContext)
	}
}

func TestAssemblerInfersProjectFromRequestContext(t *testing.T) {
	db := openAssemblerTestDB(t)
	project := domain.Project{Name: "Jarvis", Role: "owner", Status: "active", Priority: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := db.Create(&domain.PrincipalProfile{OpenID: "ou_me", Name: "我"}).Error; err != nil {
		t.Fatalf("create principal: %v", err)
	}
	assembler, err := NewAssembler(db, "ou_me")
	if err != nil {
		t.Fatalf("NewAssembler() error = %v", err)
	}
	request := json.RawMessage(fmt.Sprintf(`{"project":{"id":%d},"note":"定时执行"}`, project.ID))
	raw, err := assembler.Assemble(t.Context(), AssembleOptions{RequestContext: request})
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	snapshot, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if snapshot.Project == nil || snapshot.Project.ID != project.ID {
		t.Fatalf("project = %#v", snapshot.Project)
	}
}

func openAssemblerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	createAssemblerTables(t, db)
	return db
}

func createAssemblerTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE principal_profile (
			id INTEGER PRIMARY KEY AUTOINCREMENT, open_id TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
			department TEXT, title TEXT, background TEXT, preferences TEXT,
			leader_open_id TEXT, leader_name TEXT, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE project (
			id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT, name TEXT NOT NULL, role TEXT NOT NULL,
			status TEXT NOT NULL, priority INTEGER NOT NULL, description TEXT, repos JSON,
			tech_stack JSON, key_decisions JSON, timeline JSON, notes TEXT,
			mem0_synced_at DATETIME, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE managed_resource (
			id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, resource_type TEXT NOT NULL,
			url TEXT, description TEXT, person_id INTEGER, project_id INTEGER,
			link_principal INTEGER NOT NULL, is_active INTEGER NOT NULL,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE project_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL,
			description TEXT NOT NULL, occurred_at DATETIME NOT NULL, created_at DATETIME
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create sqlite table: %v", err)
		}
	}
}
