package execute

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openCurrentWorldTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY, todo_id INTEGER, title TEXT NOT NULL DEFAULT '',
			action_type TEXT NOT NULL DEFAULT '', target TEXT NOT NULL DEFAULT '',
			background TEXT NOT NULL DEFAULT '{}', source_payload TEXT NOT NULL DEFAULT '{}',
			source_type TEXT NOT NULL DEFAULT 'manual', source_id INTEGER, occurrence_key TEXT,
			status TEXT NOT NULL, summary TEXT, execution_result TEXT, execution_supplements TEXT,
			project_id INTEGER, version INTEGER NOT NULL DEFAULT 0,
			last_progress_at DATETIME, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE todo (
			id INTEGER PRIMARY KEY, title TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '', action_type TEXT NOT NULL DEFAULT '',
			target TEXT NOT NULL DEFAULT '', context TEXT NOT NULL DEFAULT '',
			open_questions TEXT NOT NULL DEFAULT '[]', commitment_strength TEXT NOT NULL DEFAULT '',
			source_message_ids TEXT NOT NULL DEFAULT '[]', source_quote TEXT NOT NULL DEFAULT '',
			group_id INTEGER, project_id INTEGER, assigner_open_id TEXT,
			is_leader_assigned INTEGER NOT NULL DEFAULT 0, due_at DATETIME,
			status TEXT NOT NULL, dedup_fingerprint TEXT NOT NULL DEFAULT '',
			context_snapshot TEXT, extraction_result TEXT, resolution TEXT,
			revision INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 0,
			first_seen_at DATETIME NOT NULL, last_evidence_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	return db
}

func newCurrentWorldExecutor(t *testing.T, db *gorm.DB) *AgentExecutor {
	t.Helper()
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return &AgentExecutor{store: store, now: func() time.Time {
		return time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	}}
}

// TestLoadCurrentWorldKeepsFinishedTasksAndDropsSelf pins the two rules that
// make this block worth its tokens: a Task that just finished is exactly what
// M5 must see to avoid redoing it, and the Task being executed is already in
// the prompt so it must not appear again as "other work".
func TestLoadCurrentWorldKeepsFinishedTasksAndDropsSelf(t *testing.T) {
	db := openCurrentWorldTestDB(t)
	base := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO task(id, title, status, summary, project_id, last_progress_at, created_at)
		VALUES (1, '正在执行的自己', 'executing', '本轮', 44, ?, ?),
		       (2, '刚刚做完的同类事', 'done', '已经发过飞书通知', 44, ?, ?),
		       (3, '失败过一次', 'failed', '权限不足', NULL, ?, ?)`,
		base, base, base.Add(-time.Minute), base, base.Add(-2*time.Minute), base).Error; err != nil {
		t.Fatalf("insert tasks: %v", err)
	}

	executor := newCurrentWorldExecutor(t, db)
	world, err := executor.loadCurrentWorld(t.Context(), 1)
	if err != nil {
		t.Fatalf("loadCurrentWorld() error = %v", err)
	}
	if world.LoadedAt != "2026-08-15T09:00:00Z" {
		t.Fatalf("loaded_at = %q, want the run's own clock", world.LoadedAt)
	}
	statuses := map[string]string{}
	for _, task := range world.RecentTasks {
		if task.ID == 1 {
			t.Fatalf("current Task leaked into its own current_world: %#v", world.RecentTasks)
		}
		statuses[task.Status] = task.Title
	}
	if statuses["done"] != "刚刚做完的同类事" || statuses["failed"] != "失败过一次" {
		t.Fatalf("finished Tasks must stay visible, got %#v", world.RecentTasks)
	}
	if world.RecentTasks[0].Summary != "已经发过飞书通知" {
		t.Fatalf("newest-progress-first ordering broke: %#v", world.RecentTasks)
	}
}

func TestLoadCurrentWorldOnlyReturnsOpenTodos(t *testing.T) {
	db := openCurrentWorldTestDB(t)
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO todo(id, title, target, status, group_id, first_seen_at, last_evidence_at)
		VALUES (1, '待办线索', '目标一', 'extracted', 7, ?, ?),
		       (2, '观察中的线索', '目标二', 'observing', 7, ?, ?),
		       (3, '已固化', '目标三', 'materialized', 7, ?, ?)`,
		now, now, now, now.Add(-time.Minute), now, now).Error; err != nil {
		t.Fatalf("insert todos: %v", err)
	}

	executor := newCurrentWorldExecutor(t, db)
	world, err := executor.loadCurrentWorld(t.Context(), 0)
	if err != nil {
		t.Fatalf("loadCurrentWorld() error = %v", err)
	}
	if len(world.OpenTodos) != 2 {
		t.Fatalf("open_todos = %#v, want the extracted and observing rows only", world.OpenTodos)
	}
	for _, todo := range world.OpenTodos {
		if todo.Status == "materialized" {
			t.Fatalf("materialized Todo leaked in: %#v", world.OpenTodos)
		}
		if todo.GroupID == nil || *todo.GroupID != 7 {
			t.Fatalf("group binding lost: %#v", todo)
		}
	}
}

// TestExecutionPromptSeparatesFrozenBackgroundFromCurrentWorld pins that the
// two context blocks stay distinguishable. If they merged, M5 could not tell
// which parts are creation-time evidence and which are true right now.
func TestExecutionPromptSeparatesFrozenBackgroundFromCurrentWorld(t *testing.T) {
	task := &domain.Task{
		ID: 9, Title: "发提醒", ActionType: "summary_post",
		SourcePayload: datatypes.JSON(`{"steps":["send"]}`),
		Background:    datatypes.JSON(`{"snapshot_version":"v1","captured_at":"2026-08-01T00:00:00Z"}`),
	}
	in := testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil)
	in.CurrentWorld = &currentWorld{
		LoadedAt:    "2026-08-15T09:00:00Z",
		RecentTasks: []taskBrief{{ID: 8, Title: "同一个群的提醒", Status: "done", Summary: "昨天已经发过"}},
		OpenTodos:   []todoBrief{{ID: 5, Title: "还没处理的线索", Status: "observing"}},
	}
	prompt, err := buildExecutionPrompt(in)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"execution_context":{`, `"captured_at":"2026-08-01T00:00:00Z"`,
		`"current_world":{`, `"loaded_at":"2026-08-15T09:00:00Z"`,
		"昨天已经发过", "还没处理的线索",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Index(prompt, `"execution_context"`) == strings.Index(prompt, `"current_world"`) {
		t.Fatal("execution prompt merged the frozen and live context blocks")
	}
}

// TestListTasksScopesByGroupThroughSourceTodo covers the subquery behind
// `list-tasks --group-id`. A Task carries no group of its own, so the scope has
// to travel through todo_id — and Tasks with no Todo (manual, scheduled,
// proactive) correctly fall outside any group.
func TestListTasksScopesByGroupThroughSourceTodo(t *testing.T) {
	db := openCurrentWorldTestDB(t)
	if err := db.Exec(`CREATE TABLE task_event (
		id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL,
		task_version INTEGER NOT NULL, event_type TEXT NOT NULL, from_status TEXT,
		to_status TEXT NOT NULL, actor_type TEXT NOT NULL, actor_ref TEXT, run_id INTEGER,
		detail TEXT, occurred_at DATETIME NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		t.Fatalf("create task_event: %v", err)
	}
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO todo(id, title, status, group_id, project_id, first_seen_at, last_evidence_at)
		VALUES (100, '群 7 的线索', 'materialized', 7, 44, ?, ?),
		       (200, '群 9 的线索', 'materialized', 9, 44, ?, ?)`,
		now, now, now, now).Error; err != nil {
		t.Fatalf("insert todos: %v", err)
	}
	if err := db.Exec(`INSERT INTO task(id, todo_id, title, status, project_id, created_at)
		VALUES (1, 100, '群 7 的任务', 'pending', 44, ?),
		       (2, 200, '群 9 的任务', 'pending', 44, ?),
		       (3, NULL, '手工任务无群', 'pending', 44, ?)`, now, now, now).Error; err != nil {
		t.Fatalf("insert tasks: %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	groupID := uint64(7)
	scoped, err := store.ListTasks(t.Context(), TaskFilter{
		Statuses: []string{"pending"}, GroupID: &groupID, Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListTasks(group) error = %v", err)
	}
	if scoped.Total != 1 || len(scoped.Items) != 1 || scoped.Items[0].ID != 1 {
		t.Fatalf("group scope = total %d items %#v", scoped.Total, scoped.Items)
	}

	projectID := uint64(44)
	byProject, err := store.ListTasks(t.Context(), TaskFilter{
		Statuses: []string{"pending"}, ProjectID: &projectID, Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListTasks(project) error = %v", err)
	}
	if byProject.Total != 3 {
		t.Fatalf("project scope total = %d, want all three", byProject.Total)
	}
}
