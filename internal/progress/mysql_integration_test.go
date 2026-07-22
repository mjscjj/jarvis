package progress_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/execute"
	"jarvis/internal/progress"
	"jarvis/internal/store"

	"gorm.io/datatypes"
)

func TestProgressEventsMySQL(t *testing.T) {
	dsn := os.Getenv("JARVIS_PROGRESS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JARVIS_PROGRESS_TEST_MYSQL_DSN is required")
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

	projectService, err := background.NewProjectService(db)
	if err != nil {
		t.Fatalf("NewProjectService() error = %v", err)
	}
	project, err := projectService.Create(context.Background(), background.ProjectInput{
		Name: "Jarvis", Role: "owner", Status: "planning", Priority: 1,
	})
	if err != nil {
		t.Fatalf("Create project error = %v", err)
	}
	description := "主动式助手"
	project, err = projectService.Update(context.Background(), project.ID, background.ProjectInput{
		Name: "Jarvis", Role: "owner", Status: "active", Priority: 1, Description: &description,
	})
	if err != nil {
		t.Fatalf("Update project error = %v", err)
	}
	if err := projectService.Delete(context.Background(), project.ID); err != nil {
		t.Fatalf("archive project error = %v", err)
	}
	eventService, err := progress.NewService(db)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	projectEvents, err := eventService.ListProjectEvents(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("ListProjectEvents() error = %v", err)
	}
	if len(projectEvents) != 4 {
		t.Fatalf("project event count = %d, want 4: %#v", len(projectEvents), projectEvents)
	}

	now := time.Now().UTC()
	todo := domain.Todo{
		Title: "实现存储", Description: "实现事件存储", ActionType: "code_change",
		Target: "jarvis", Context: "integration", OpenQuestions: datatypes.JSON(`[]`),
		CommitmentStrength: "firm", SourceMessageIDs: datatypes.JSON(`[]`), SourceQuote: "test",
		Status: "confirmed", DedupFingerprint: strings.Repeat("a", 64),
		ExtractionModel: "test", PromptVersion: "test", FirstSeenAt: now, LastEvidenceAt: now,
	}
	if err := db.Create(&todo).Error; err != nil {
		t.Fatalf("create Todo: %v", err)
	}
	task := domain.Task{
		TodoID: todo.ID, Title: todo.Title, ActionType: todo.ActionType,
		Background: datatypes.JSON(`{}`), Plan: datatypes.JSON(`{"steps":["test"]}`),
		ConfirmedBy: "user", ConfirmedAt: now, ActionHash: strings.Repeat("b", 64),
		Status: "pending", AutonomyMode: "copilot",
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create Task: %v", err)
	}
	if err := progress.AppendTaskEvent(db, progress.TaskEventInput{
		TaskID: task.ID, TaskVersion: 0, EventType: "created",
		ToStatus: "pending", ActorType: "user", OccurredAt: now,
	}); err != nil {
		t.Fatalf("append created event: %v", err)
	}
	executionStore, err := execute.NewStore(db)
	if err != nil {
		t.Fatalf("execute.NewStore() error = %v", err)
	}
	executingVersion, err := executionStore.MarkExecuting(context.Background(), task.ID, 0)
	if err != nil {
		t.Fatalf("MarkExecuting() error = %v", err)
	}
	run := domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Sandbox: "read-only",
		Status: "succeeded", Prompt: "test", StartedAt: now,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create ExecutionRun: %v", err)
	}
	approvalVersion, err := executionStore.MarkAwaitingApproval(
		context.Background(), task.ID, executingVersion, run.ID, json.RawMessage(`{"proposal":{"action":"test"}}`),
	)
	if err != nil {
		t.Fatalf("MarkAwaitingApproval() error = %v", err)
	}
	applyVersion, err := executionStore.MarkExecutingFromApproval(context.Background(), task.ID, approvalVersion)
	if err != nil {
		t.Fatalf("MarkExecutingFromApproval() error = %v", err)
	}
	if _, err := executionStore.Finish(context.Background(), execute.FinishInput{
		TaskID: task.ID, ExpectedVersion: applyVersion, Status: "done",
		Result: json.RawMessage(`{"summary":"done"}`), ActorType: "m5", RunID: &run.ID,
	}); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	taskEvents, err := eventService.ListTaskEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListTaskEvents() error = %v", err)
	}
	wantTypes := []string{"execution_succeeded", "approval_granted", "approval_requested", "execution_started", "created"}
	if len(taskEvents) != len(wantTypes) {
		t.Fatalf("task events = %#v", taskEvents)
	}
	for i, want := range wantTypes {
		if taskEvents[i].EventType != want {
			t.Fatalf("task event[%d] = %q, want %q", i, taskEvents[i].EventType, want)
		}
	}
}
