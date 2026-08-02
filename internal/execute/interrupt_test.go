package execute

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestActiveExecutionLifecycle(t *testing.T) {
	executor := &AgentExecutor{}
	runCtx, active, err := executor.beginExecution(t.Context(), 54)
	if err != nil {
		t.Fatalf("beginExecution() error = %v", err)
	}
	if _, _, err := executor.beginExecution(t.Context(), 54); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("duplicate beginExecution() error = %v, want ErrInvalidTransition", err)
	}

	active.cancel(ErrExecutionInterrupted)
	if !errors.Is(context.Cause(runCtx), ErrExecutionInterrupted) {
		t.Fatalf("run context cause = %v, want ErrExecutionInterrupted", context.Cause(runCtx))
	}
	executor.endExecution(54, active)
	select {
	case <-active.done:
	default:
		t.Fatal("active execution done channel was not closed")
	}

	_, next, err := executor.beginExecution(t.Context(), 54)
	if err != nil {
		t.Fatalf("beginExecution() after end error = %v", err)
	}
	executor.abandonExecution(54, next)
}

func TestInterruptInactiveExecutionMarksTaskFailed(t *testing.T) {
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
			source_type TEXT NOT NULL DEFAULT 'manual',
			source_id INTEGER, occurrence_key TEXT,
			status TEXT NOT NULL, execution_result TEXT, execution_supplements TEXT,
			project_id INTEGER, version INTEGER NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE scheduled_task (
			id INTEGER PRIMARY KEY, dispatch_kind TEXT, subject_type TEXT, subject_id INTEGER,
			source_run_id INTEGER, status TEXT, last_run_status TEXT, last_error_detail TEXT,
			last_finished_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL,
			task_version INTEGER NOT NULL, event_type TEXT NOT NULL, from_status TEXT,
			to_status TEXT NOT NULL, actor_type TEXT NOT NULL, actor_ref TEXT, run_id INTEGER,
			detail TEXT, occurred_at DATETIME NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(task_id, task_version)
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	if err := db.Exec("INSERT INTO task(id, status, version) VALUES (?, ?, ?)", 54, "executing", 7).Error; err != nil {
		t.Fatalf("insert Task: %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	executor := &AgentExecutor{store: store}
	result, err := executor.Interrupt(t.Context(), 54, 7)
	if err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("Interrupt() status = %q, want failed", result.Status)
	}
	var task domain.Task
	if err := db.First(&task, 54).Error; err != nil {
		t.Fatalf("load interrupted Task: %v", err)
	}
	if task.Status != "failed" || task.Version != 8 || !resultHasStage(task.ExecutionResult, "interrupted") {
		t.Fatalf("interrupted Task = status=%s version=%d result=%s", task.Status, task.Version, task.ExecutionResult)
	}
	var event domain.TaskEvent
	if err := db.Where("task_id = ?", 54).First(&event).Error; err != nil {
		t.Fatalf("load interrupt event: %v", err)
	}
	if event.EventType != "execution_interrupted" || event.ActorType != "user" || event.ToStatus != "failed" {
		t.Fatalf("interrupt event = %#v", event)
	}
}
