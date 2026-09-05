package execute

import (
	"fmt"
	"testing"
	"time"

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
			content TEXT, resolution TEXT,
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
