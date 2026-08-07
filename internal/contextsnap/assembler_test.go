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
	if err := db.Create(&domain.Fact{
		SubjectType: "project", SubjectID: project.ID,
		Description: "完成上下文链路", OccurredAt: eventAt,
	}).Error; err != nil {
		t.Fatalf("create fact: %v", err)
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
	if len(snapshot.ManagedResources) != 1 || len(snapshot.Facts) != 1 {
		t.Fatalf("resources/facts = %#v / %#v", snapshot.ManagedResources, snapshot.Facts)
	}
	if snapshot.Facts[0].SubjectType != "project" || snapshot.Facts[0].SubjectID != project.ID {
		t.Fatalf("fact subject = %#v", snapshot.Facts[0])
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

func TestAssemblerResolvesChatBackgroundAndCurrentWork(t *testing.T) {
	db := openAssemblerTestDB(t)
	project := domain.Project{Name: "Agent Runtime", Role: "owner", Status: "active", Priority: 1}
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
	if err := db.Exec(`INSERT INTO feishu_group(id, chat_id, name, background_note, project_id, is_key_group)
		VALUES (7, 'oc_runtime', 'Agent runtime 攻坚小队', '关注 Runtime 主链路', ?, 1)`, project.ID).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO todo(id, title, action_type, status, group_id, project_id, last_evidence_at)
		VALUES (11, '排查上下文', 'agent_task', 'extracted', 7, ?, ?),
		       (12, '其它项目线索', 'agent_task', 'extracted', NULL, ?, ?)`, project.ID, now, other.ID, now).Error; err != nil {
		t.Fatalf("create todos: %v", err)
	}
	if err := db.Exec(`INSERT INTO task(id, todo_id, title, status, summary, project_id, created_at)
		VALUES (21, 11, '补齐上下文', 'waiting', '等待下一轮验证', ?, ?),
		       (22, 11, '已经结束', 'done', '已完成', ?, ?),
		       (23, 12, '其它项目任务', 'pending', '', ?, ?)`,
		project.ID, now, project.ID, now, other.ID, now).Error; err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	if err := db.Create(&domain.Fact{
		SubjectType: "group", SubjectID: 7, Description: "群内要求先验证再上线", OccurredAt: now,
	}).Error; err != nil {
		t.Fatalf("create group fact: %v", err)
	}

	assembler, err := NewAssembler(db, "ou_me")
	if err != nil {
		t.Fatalf("NewAssembler() error = %v", err)
	}
	raw, err := assembler.AssembleConversation(t.Context(), AssembleOptions{
		RequestContext: json.RawMessage(`{"chat_id":"oc_runtime","message_id":"om_1"}`),
	})
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	snapshot, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if snapshot.Group == nil || snapshot.Group.ChatID != "oc_runtime" || snapshot.Group.BackgroundNote == nil || *snapshot.Group.BackgroundNote != "关注 Runtime 主链路" {
		t.Fatalf("group = %#v", snapshot.Group)
	}
	if snapshot.Project == nil || snapshot.Project.ID != project.ID {
		t.Fatalf("project = %#v", snapshot.Project)
	}
	if len(snapshot.OpenTodos) != 1 || snapshot.OpenTodos[0].ID != 11 {
		t.Fatalf("open_todos = %#v", snapshot.OpenTodos)
	}
	if len(snapshot.RecentTasks) != 1 || snapshot.RecentTasks[0].ID != 21 || snapshot.RecentTasks[0].Summary != "等待下一轮验证" {
		t.Fatalf("recent_tasks = %#v", snapshot.RecentTasks)
	}
	if len(snapshot.Facts) != 1 || snapshot.Facts[0].SubjectType != "group" {
		t.Fatalf("facts = %#v", snapshot.Facts)
	}
}

func TestAssemblerRejectsUnknownChat(t *testing.T) {
	db := openAssemblerTestDB(t)
	if err := db.Create(&domain.PrincipalProfile{OpenID: "ou_me", Name: "我"}).Error; err != nil {
		t.Fatalf("create principal: %v", err)
	}
	assembler, err := NewAssembler(db, "ou_me")
	if err != nil {
		t.Fatalf("NewAssembler() error = %v", err)
	}
	if _, err := assembler.AssembleConversation(t.Context(), AssembleOptions{ChatID: "oc_missing"}); err == nil {
		t.Fatal("Assemble() error = nil, want unknown chat failure")
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
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE managed_resource (
			id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, resource_type TEXT NOT NULL,
			url TEXT, description TEXT, person_id INTEGER, project_id INTEGER,
			link_principal INTEGER NOT NULL, is_active INTEGER NOT NULL,
			last_active_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE fact (
			id INTEGER PRIMARY KEY AUTOINCREMENT, subject_type TEXT NOT NULL, subject_id INTEGER NOT NULL,
			description TEXT NOT NULL, occurred_at DATETIME NOT NULL,
			source_kind TEXT, source_id INTEGER, created_at DATETIME
		)`,
		`CREATE TABLE feishu_group (
			id INTEGER PRIMARY KEY AUTOINCREMENT, chat_id TEXT NOT NULL UNIQUE,
			name TEXT, description TEXT, background_note TEXT, project_id INTEGER,
			is_key_group INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE todo (
			id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL,
			action_type TEXT NOT NULL, status TEXT NOT NULL, group_id INTEGER,
			project_id INTEGER, last_evidence_at DATETIME
		)`,
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY AUTOINCREMENT, todo_id INTEGER, title TEXT NOT NULL,
			status TEXT NOT NULL, summary TEXT, project_id INTEGER,
			last_progress_at DATETIME, created_at DATETIME
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create sqlite table: %v", err)
		}
	}
}
