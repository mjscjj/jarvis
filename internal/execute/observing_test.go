package execute

import (
	"encoding/json"
	"fmt"
	"testing"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestParseExecutionResultObserving accepts the verdict "I investigated and
// nobody needs to act". It carries no failure_reason and no waiting.
func TestParseExecutionResultObserving(t *testing.T) {
	msg := `{"needs_approval":false,"outcome":"observing","progress_summary":"群里已就口径达成一致，无人需要动手","summary":"核验后确认结论已达成","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null,"effects":[],"waiting":null}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if result.Outcome != "observing" || result.NeedsApproval {
		t.Fatalf("result = %#v", result)
	}
}

// TestParseExecutionResultObservingRejectsWaiting keeps observing and waiting
// distinct: observing means there is no condition to wait on.
func TestParseExecutionResultObservingRejectsWaiting(t *testing.T) {
	msg := `{"needs_approval":false,"outcome":"observing","progress_summary":"","summary":"无人需要动手","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null,"effects":[],"waiting":{"scheduled_task_id":42,"wake_at":"2026-08-03T10:00:00+08:00","reason":"稍后再看"}}`
	if _, err := parseExecutionResult(msg); err == nil {
		t.Fatal("outcome=observing with a waiting block must fail")
	}
}

func newObservingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Hand-written DDL keeps this test limited to the columns it exercises.
	for _, statement := range []string{
		`CREATE TABLE todo (
			id INTEGER PRIMARY KEY, title TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
			action_type TEXT NOT NULL DEFAULT '', commitment_strength TEXT NOT NULL DEFAULT '',
			source_message_ids TEXT NOT NULL DEFAULT '[]', source_quote TEXT NOT NULL DEFAULT '',
			group_id INTEGER, project_id INTEGER, assigner_open_id TEXT,
			is_leader_assigned BOOLEAN NOT NULL DEFAULT 0, due_at DATETIME,
			status TEXT NOT NULL DEFAULT 'extracted', dedup_fingerprint TEXT NOT NULL,
			revision INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 0,
			first_seen_at DATETIME NOT NULL, last_evidence_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			context_snapshot TEXT, resolution TEXT, target TEXT NOT NULL DEFAULT '',
			context TEXT NOT NULL DEFAULT '', open_questions TEXT NOT NULL DEFAULT '[]',
			extraction_result TEXT
		)`,
		`CREATE TABLE todo_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, todo_id INTEGER NOT NULL, from_status TEXT,
			to_status TEXT NOT NULL, actor TEXT NOT NULL, detail TEXT, snapshot TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY, todo_id INTEGER, title TEXT NOT NULL DEFAULT '',
			action_type TEXT NOT NULL DEFAULT '', target TEXT NOT NULL DEFAULT '',
			background TEXT NOT NULL DEFAULT '{}', source_payload TEXT NOT NULL DEFAULT '{}',
			source_type TEXT NOT NULL DEFAULT 'manual', source_id INTEGER, occurrence_key TEXT,
			status TEXT NOT NULL,
			execution_result TEXT, execution_supplements TEXT,
			summary TEXT, last_progress_at DATETIME,
			project_id INTEGER, version INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL,
			task_version INTEGER NOT NULL, event_type TEXT NOT NULL, from_status TEXT,
			to_status TEXT NOT NULL, actor_type TEXT NOT NULL, actor_ref TEXT, run_id INTEGER,
			detail TEXT, occurred_at DATETIME NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(task_id, task_version)
		)`,
		`CREATE TABLE scheduled_task (
			id INTEGER PRIMARY KEY, dispatch_kind TEXT, subject_type TEXT, subject_id INTEGER,
			source_run_id INTEGER, status TEXT, last_run_status TEXT, last_error_detail TEXT,
			last_finished_at DATETIME, updated_at DATETIME
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	return db
}

func insertObservingFixture(t *testing.T, db *gorm.DB, todoStatus string) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO todo(id, title, description, action_type, commitment_strength, source_message_ids,
			source_quote, status, dedup_fingerprint, revision, version, first_seen_at, last_evidence_at,
			target, context, open_questions)
		 VALUES (7, '群里已达成的口径', '', 'notify_principal', 'mentioned', '[]', 'quote', ?, 'fp7', 1, 3,
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, '评测口径', '', '[]')`,
		todoStatus,
	).Error; err != nil {
		t.Fatalf("insert Todo: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO task(id, todo_id, title, action_type, background, source_payload,
			status, version, target, source_type)
		 VALUES (11, 7, '同步口径', 'notify_principal', '{}', '{}',
			'executing', 2, '评测口径', 'todo')`,
	).Error; err != nil {
		t.Fatalf("insert Task: %v", err)
	}
}

// TestFinishObservingParksClue is the whole point of the execution-stage
// observing outcome: the Task lands terminal without claiming completion, and
// the clue goes back to observing so dedup keeps treating it as live.
func TestFinishObservingParksClue(t *testing.T) {
	db := newObservingTestDB(t)
	insertObservingFixture(t, db, "materialized")
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	view, err := store.Finish(t.Context(), FinishInput{
		TaskID: 11, ExpectedVersion: 2, Status: "observing",
		Result:    json.RawMessage(`{"stage":"executed","outcome":"observing","summary":"结论已达成，无人需要动手"}`),
		ActorType: "m5",
	})
	if err != nil {
		t.Fatalf("Finish(observing) error = %v", err)
	}
	if view.Status != "observing" || view.Version != 3 {
		t.Fatalf("task view = %#v", view)
	}
	var todo domain.Todo
	if err := db.First(&todo, 7).Error; err != nil {
		t.Fatalf("load Todo: %v", err)
	}
	if todo.Status != "observing" || todo.Version != 4 {
		t.Fatalf("todo status=%s version=%d, want observing/4", todo.Status, todo.Version)
	}
	var todoEvent domain.TodoEvent
	if err := db.Where("todo_id = ?", 7).Take(&todoEvent).Error; err != nil {
		t.Fatalf("load Todo event: %v", err)
	}
	if todoEvent.ToStatus != "observing" || todoEvent.Actor != "m5" {
		t.Fatalf("todo event = %#v", todoEvent)
	}
	var taskEvent domain.TaskEvent
	if err := db.Where("task_id = ?", 11).Take(&taskEvent).Error; err != nil {
		t.Fatalf("load Task event: %v", err)
	}
	if taskEvent.EventType != "execution_observing" || taskEvent.ToStatus != "observing" {
		t.Fatalf("task event = %#v", taskEvent)
	}
}

// TestFinishObservingRejectsUnexpectedClueStatus fails fast rather than
// silently rewriting a clue that is not where the pipeline expects it.
func TestFinishObservingRejectsUnexpectedClueStatus(t *testing.T) {
	db := newObservingTestDB(t)
	insertObservingFixture(t, db, "extracted")
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if _, err := store.Finish(t.Context(), FinishInput{
		TaskID: 11, ExpectedVersion: 2, Status: "observing",
		Result:    json.RawMessage(`{"stage":"executed"}`),
		ActorType: "m5",
	}); err == nil {
		t.Fatal("finishing observing from an unmaterialized clue must fail")
	}
	var task domain.Task
	if err := db.First(&task, 11).Error; err != nil {
		t.Fatalf("load Task: %v", err)
	}
	if task.Status != "executing" {
		t.Fatalf("task status = %q, want the rejected transition to leave it executing", task.Status)
	}
}

// TestFinishObservingWithoutClue covers Tasks that never came from M3 (a
// scheduled run, say): there is simply no clue to park.
func TestFinishObservingWithoutClue(t *testing.T) {
	db := newObservingTestDB(t)
	if err := db.Exec(
		`INSERT INTO task(id, todo_id, title, action_type, background, source_payload,
			status, version, target, source_type)
		 VALUES (12, NULL, '定时巡检', 'investigate', '{}', '{}',
			'executing', 0, '巡检', 'scheduled_task')`,
	).Error; err != nil {
		t.Fatalf("insert Task: %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	view, err := store.Finish(t.Context(), FinishInput{
		TaskID: 12, ExpectedVersion: 0, Status: "observing",
		Result:    json.RawMessage(`{"stage":"executed"}`),
		ActorType: "m5",
	})
	if err != nil {
		t.Fatalf("Finish(observing) error = %v", err)
	}
	if view.Status != "observing" {
		t.Fatalf("task view = %#v", view)
	}
}
