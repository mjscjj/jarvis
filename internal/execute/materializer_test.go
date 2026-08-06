package execute

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/datatypes"
)

func TestMaterializeTodoCarriesExtractionAsSourcePayload(t *testing.T) {
	db := newMaterializerTestDB(t)
	insertMaterializerTodo(t, db, 7, 3)
	materializer, err := NewMaterializer(db)
	if err != nil {
		t.Fatal(err)
	}

	result, err := materializer.MaterializeTodo(context.Background(), 7, 3)
	if err != nil {
		t.Fatalf("MaterializeTodo() error = %v", err)
	}
	if result.TodoID != 7 || result.TodoVersion != 4 || result.TaskID == 0 || result.TaskVersion != 0 {
		t.Fatalf("result = %#v", result)
	}
	var task domain.Task
	if err := db.First(&task, result.TaskID).Error; err != nil {
		t.Fatal(err)
	}
	if task.TodoID == nil || *task.TodoID != 7 || string(task.SourcePayload) != `{"desired_outcome":"完成目标"}` || task.RepoPath != nil {
		t.Fatalf("task = %#v source_payload=%s", task, task.SourcePayload)
	}
	var todo domain.Todo
	if err := db.First(&todo, 7).Error; err != nil {
		t.Fatal(err)
	}
	if todo.Status != "materialized" || todo.Version != 4 {
		t.Fatalf("todo status=%s version=%d", todo.Status, todo.Version)
	}
	var todoEvent domain.TodoEvent
	if err := db.Where("todo_id = ?", 7).Take(&todoEvent).Error; err != nil {
		t.Fatal(err)
	}
	if todoEvent.Actor != "materializer" || todoEvent.FromStatus == nil || *todoEvent.FromStatus != "extracted" || todoEvent.ToStatus != "materialized" {
		t.Fatalf("todo event = %#v", todoEvent)
	}
	var taskEvent domain.TaskEvent
	if err := db.Where("task_id = ?", result.TaskID).Take(&taskEvent).Error; err != nil {
		t.Fatal(err)
	}
	if taskEvent.ActorType != "system" || taskEvent.EventType != "created" {
		t.Fatalf("task event = %#v", taskEvent)
	}
}

func TestMaterializeTodoDuplicateNotificationIsIdempotent(t *testing.T) {
	db := newMaterializerTestDB(t)
	insertMaterializerTodo(t, db, 8, 2)
	materializer, err := NewMaterializer(db)
	if err != nil {
		t.Fatal(err)
	}

	first, err := materializer.MaterializeTodo(context.Background(), 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := materializer.MaterializeTodo(context.Background(), 8, 2)
	if err != nil {
		t.Fatalf("duplicate MaterializeTodo() error = %v", err)
	}
	if *first != *second {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	var count int64
	if err := db.Model(&domain.Task{}).Where("todo_id = ?", 8).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("Task count = %d", count)
	}
}

func TestMaterializeTodoFreshEvidenceRerunsExistingObservingTask(t *testing.T) {
	db := newMaterializerTestDB(t)
	insertMaterializerTodo(t, db, 10, 4)
	todoID := uint64(10)
	task := domain.Task{
		ID: 56, TodoID: &todoID, Title: "已有执行任务", ActionType: "investigate", Target: "目标",
		Background:    datatypes.JSON(`{"snapshot_version":"v1","captured_at":"2026-08-02T12:00:00Z"}`),
		SourcePayload: datatypes.JSON(`{"original":"evidence"}`), SourceType: "todo", SourceID: &todoID,
		Status: "observing", ExecutionResult: datatypes.JSON(`{"outcome":"observing"}`), Version: 6,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	materializer, err := NewMaterializer(db)
	if err != nil {
		t.Fatal(err)
	}

	result, err := materializer.MaterializeTodo(context.Background(), todoID, 4)
	if err != nil {
		t.Fatalf("MaterializeTodo() error = %v", err)
	}
	if result.TodoID != todoID || result.TodoVersion != 5 || result.TaskID != 56 || result.TaskVersion != 7 {
		t.Fatalf("result = %#v", result)
	}
	var reloadedTask domain.Task
	if err := db.First(&reloadedTask, 56).Error; err != nil {
		t.Fatal(err)
	}
	if reloadedTask.Status != "pending" || reloadedTask.Version != 7 || len(reloadedTask.ExecutionResult) != 0 {
		t.Fatalf("Task status=%s version=%d execution_result=%s", reloadedTask.Status, reloadedTask.Version, reloadedTask.ExecutionResult)
	}
	if string(reloadedTask.SourcePayload) != `{"original":"evidence"}` || string(reloadedTask.Background) != `{"snapshot_version":"v1","captured_at":"2026-08-02T12:00:00Z"}` {
		t.Fatalf("frozen Task evidence changed: source_payload=%s background=%s", reloadedTask.SourcePayload, reloadedTask.Background)
	}
	var todo domain.Todo
	if err := db.First(&todo, todoID).Error; err != nil {
		t.Fatal(err)
	}
	if todo.Status != "materialized" || todo.Version != 5 {
		t.Fatalf("Todo status=%s version=%d", todo.Status, todo.Version)
	}
	var taskEvent domain.TaskEvent
	if err := db.Where("task_id = ? AND task_version = ?", 56, 7).Take(&taskEvent).Error; err != nil {
		t.Fatal(err)
	}
	if taskEvent.EventType != "rerun_requested" || taskEvent.ActorType != "system" || taskEvent.FromStatus == nil || *taskEvent.FromStatus != "observing" || taskEvent.ToStatus != "pending" {
		t.Fatalf("Task event = %#v", taskEvent)
	}
	var todoEvent domain.TodoEvent
	if err := db.Where("todo_id = ?", todoID).Take(&todoEvent).Error; err != nil {
		t.Fatal(err)
	}
	if todoEvent.Actor != "materializer" || todoEvent.FromStatus == nil || *todoEvent.FromStatus != "extracted" || todoEvent.ToStatus != "materialized" {
		t.Fatalf("Todo event = %#v", todoEvent)
	}

	duplicate, err := materializer.MaterializeTodo(context.Background(), todoID, 4)
	if err != nil {
		t.Fatalf("duplicate MaterializeTodo() error = %v", err)
	}
	if *duplicate != *result {
		t.Fatalf("first=%#v duplicate=%#v", result, duplicate)
	}
	var count int64
	if err := db.Model(&domain.Task{}).Where("todo_id = ?", todoID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("Task count = %d", count)
	}
}

func TestMaterializeTodoRejectsExistingNonObservingTask(t *testing.T) {
	db := newMaterializerTestDB(t)
	insertMaterializerTodo(t, db, 11, 2)
	todoID := uint64(11)
	task := domain.Task{
		TodoID: &todoID, Title: "正在执行的任务", ActionType: "investigate", Target: "目标",
		Background: datatypes.JSON(`{}`), SourcePayload: datatypes.JSON(`{}`),
		SourceType: "todo", SourceID: &todoID, Status: "pending", Version: 3,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	materializer, err := NewMaterializer(db)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := materializer.MaterializeTodo(context.Background(), todoID, 2); !errors.Is(err, ErrTaskExists) {
		t.Fatalf("MaterializeTodo() error = %v, want ErrTaskExists", err)
	}
	var todo domain.Todo
	if err := db.First(&todo, todoID).Error; err != nil {
		t.Fatal(err)
	}
	if todo.Status != "extracted" || todo.Version != 2 {
		t.Fatalf("Todo changed after rejected rematerialization: status=%s version=%d", todo.Status, todo.Version)
	}
}

func TestMaterializeTodoRejectsVersionConflict(t *testing.T) {
	db := newMaterializerTestDB(t)
	insertMaterializerTodo(t, db, 9, 5)
	materializer, err := NewMaterializer(db)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := materializer.MaterializeTodo(context.Background(), 9, 4); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("error = %v, want ErrVersionConflict", err)
	}
}

func newMaterializerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE todo (
			id INTEGER PRIMARY KEY, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
			action_type TEXT NOT NULL, target TEXT NOT NULL, context TEXT NOT NULL DEFAULT '',
			open_questions TEXT NOT NULL DEFAULT '[]', commitment_strength TEXT NOT NULL DEFAULT '',
			source_message_ids TEXT NOT NULL DEFAULT '[]', source_quote TEXT NOT NULL DEFAULT '',
			group_id INTEGER, project_id INTEGER, assigner_open_id TEXT, is_leader_assigned BOOLEAN NOT NULL DEFAULT 0,
			due_at DATETIME, status TEXT NOT NULL, dedup_fingerprint TEXT NOT NULL,
			context_snapshot TEXT, extraction_result TEXT, resolution TEXT,
			revision INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL,
			first_seen_at DATETIME NOT NULL, last_evidence_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE todo_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, todo_id INTEGER NOT NULL, from_status TEXT,
			to_status TEXT NOT NULL, actor TEXT NOT NULL, detail TEXT, snapshot TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY AUTOINCREMENT, todo_id INTEGER UNIQUE, title TEXT NOT NULL,
			action_type TEXT NOT NULL, target TEXT NOT NULL, background TEXT NOT NULL,
			source_payload TEXT NOT NULL, source_type TEXT NOT NULL, source_id INTEGER,
			occurrence_key TEXT, status TEXT NOT NULL,
			execution_result TEXT, summary TEXT, last_progress_at DATETIME, execution_supplements TEXT,
			project_id INTEGER, repo_path TEXT, version INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL, task_version INTEGER NOT NULL,
			event_type TEXT NOT NULL, from_status TEXT, to_status TEXT NOT NULL,
			actor_type TEXT NOT NULL, actor_ref TEXT, run_id INTEGER, detail TEXT,
			occurred_at DATETIME NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(task_id, task_version)
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func insertMaterializerTodo(t *testing.T, db *gorm.DB, id uint64, version int32) {
	t.Helper()
	now := time.Now().UTC()
	todo := domain.Todo{
		ID: id, Title: "执行线索", ActionType: "investigate", Target: "目标",
		Status: "extracted", DedupFingerprint: fmt.Sprintf("fp-%d", id),
		OpenQuestions:    datatypes.JSON(`[]`),
		SourceMessageIDs: datatypes.JSON(`[]`),
		ContextSnapshot:  datatypes.JSON(`{"snapshot_version":"v1","captured_at":"2026-08-02T12:00:00Z","principal":{"open_id":"ou_owner","name":"Owner"},"project":{"id":1,"name":"Jarvis","role":"owner","repos":[{"local_path":"jarvis"}]},"messages":[],"memories":[],"facts":[],"recent_tasks":[],"open_todos":[]}`),
		ExtractionResult: datatypes.JSON(`{"desired_outcome":"完成目标"}`),
		Revision:         1, Version: version, FirstSeenAt: now, LastEvidenceAt: now,
	}
	if err := db.Create(&todo).Error; err != nil {
		t.Fatal(err)
	}
}
