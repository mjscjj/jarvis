package domain

import "testing"

func TestProgressModels(t *testing.T) {
	t.Parallel()
	models := ProgressModels()
	if got, want := len(models), 2; got != want {
		t.Fatalf("ProgressModels() length = %d, want %d", got, want)
	}
	if got := models[0].(*TaskEvent).TableName(); got != "task_event" {
		t.Fatalf("TaskEvent table = %q, want task_event", got)
	}
	if got := models[1].(*ProjectEvent).TableName(); got != "project_event" {
		t.Fatalf("ProjectEvent table = %q, want project_event", got)
	}
}
