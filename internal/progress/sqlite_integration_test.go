//go:build integration

package progress_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/execute"
	"jarvis/internal/progress"
	"jarvis/internal/store"

	"jarvis/internal/datatypes"
)

func TestProgressEventsSQLite(t *testing.T) {
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
	facts, err := eventService.ListFacts(context.Background(), progress.FactFilter{
		SubjectType: "project", SubjectID: project.ID,
	})
	if err != nil {
		t.Fatalf("ListFacts() error = %v", err)
	}
	if len(facts) != 4 {
		t.Fatalf("fact count = %d, want 4: %#v", len(facts), facts)
	}
	for _, fact := range facts {
		if fact.Description == "" {
			t.Fatalf("fact has empty description: %#v", fact)
		}
		if fact.SubjectType != "project" || fact.SubjectID != project.ID {
			t.Fatalf("fact subject = %#v", fact)
		}
	}

	now := time.Now().UTC()
	todo := domain.Todo{
		Title: "实现存储", Description: "实现事件存储", ActionType: "code_change",
		Target: "jarvis", Context: "integration", OpenQuestions: datatypes.JSON(`[]`),
		CommitmentStrength: "firm", SourceMessageIDs: datatypes.JSON(`[]`), SourceQuote: "test",
		Status: "materialized", DedupFingerprint: strings.Repeat("a", 64),
		FirstSeenAt: now, LastEvidenceAt: now,
	}
	if err := db.Create(&todo).Error; err != nil {
		t.Fatalf("create Todo: %v", err)
	}
	task := domain.Task{
		TodoID: &todo.ID, Title: todo.Title, ActionType: todo.ActionType,
		Background: datatypes.JSON(`{}`), SourcePayload: datatypes.JSON(`{"steps":["test"]}`),
		Status: "pending"}
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
