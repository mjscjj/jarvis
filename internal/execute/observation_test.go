package execute

import (
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
)

func runWithOutput(t *testing.T, output string) *domain.ExecutionRun {
	t.Helper()
	started := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	return &domain.ExecutionRun{ID: 7, TaskID: 3, StartedAt: started, Output: datatypes.JSON(output)}
}

func TestRunObservationsMapsEnrichments(t *testing.T) {
	t.Parallel()
	projectID := uint64(11)
	task := &domain.Task{ID: 3, ProjectID: &projectID}
	run := runWithOutput(t, `{"enrichments":[
		{"kind":"repo_fact","label":"agent-runtime 构建方式","content":"这个仓库要先跑 make gen 才能编译。"}
	]}`)

	rows, err := runObservations(task, run, time.UTC)
	if err != nil {
		t.Fatalf("runObservations() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want 1", rows)
	}
	row := rows[0]
	if row.Producer != domain.ObservationProducerM5 {
		t.Fatalf("producer = %q", row.Producer)
	}
	if row.Subject != "agent-runtime 构建方式" || row.Content != "这个仓库要先跑 make gen 才能编译。" {
		t.Fatalf("row = %#v", row)
	}
	if row.SourceRunID == nil || *row.SourceRunID != 7 {
		t.Fatalf("source_run_id = %v, want 7", row.SourceRunID)
	}
	if row.ProjectID == nil || *row.ProjectID != projectID {
		t.Fatalf("project_id = %v, want %d", row.ProjectID, projectID)
	}
}

func TestRunObservationsSkipsIncompleteEntries(t *testing.T) {
	t.Parallel()
	task := &domain.Task{ID: 3}
	run := runWithOutput(t, `{"enrichments":[
		{"kind":"note","label":"","content":"没有主题"},
		{"kind":"note","label":"有主题","content":"   "}
	]}`)

	rows, err := runObservations(task, run, time.UTC)
	if err != nil {
		t.Fatalf("runObservations() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want none", rows)
	}
}

// Re-running the same Task rediscovers the same facts; those must collapse.
// Different facts from the same Task must not.
func TestExecutionObservationDedupKey(t *testing.T) {
	t.Parallel()
	task := &domain.Task{ID: 3}
	other := &domain.Task{ID: 4}

	same := executionObservationDedupKey(task, "构建方式", "先跑 make gen")
	repeat := executionObservationDedupKey(task, " 构建方式 ", "先跑  make gen")
	if same != repeat {
		t.Fatalf("same fact produced different keys: %s vs %s", same, repeat)
	}
	if other := executionObservationDedupKey(task, "构建方式", "还要装 protoc"); same == other {
		t.Fatalf("different facts collapsed onto key %s", same)
	}
	if crossTask := executionObservationDedupKey(other, "构建方式", "先跑 make gen"); same == crossTask {
		t.Fatalf("same fact from two Tasks collapsed onto key %s", same)
	}
}

// A run whose output we cannot parse must not break finishing the Task.
func TestRunObservationsTolerlatesUnreadableOutput(t *testing.T) {
	t.Parallel()
	task := &domain.Task{ID: 3}
	run := runWithOutput(t, `not json`)

	rows, err := runObservations(task, run, time.UTC)
	if err != nil {
		t.Fatalf("runObservations() error = %v", err)
	}
	if rows != nil {
		t.Fatalf("rows = %#v, want nil", rows)
	}
}
