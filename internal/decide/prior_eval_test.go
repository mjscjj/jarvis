package decide

import (
	"context"
	"testing"
)

func TestLoadPriorEvaluationsNilDB(t *testing.T) {
	got, err := loadPriorEvaluations(context.Background(), nil, 1)
	if err != nil {
		t.Fatalf("nil db must not error: %v", err)
	}
	if got != nil {
		t.Fatalf("nil db must yield empty prior list, got %#v", got)
	}
}

func TestLoadPriorEvaluationsZeroTodoID(t *testing.T) {
	got, err := loadPriorEvaluations(context.Background(), nil, 0)
	if err != nil {
		t.Fatalf("zero todo_id must not error: %v", err)
	}
	if got != nil {
		t.Fatalf("zero todo_id must yield empty prior list, got %#v", got)
	}
}
