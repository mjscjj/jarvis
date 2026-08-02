package extract

import (
	"context"
	"strings"
	"testing"
)

func TestValidateTodoFilter(t *testing.T) {
	valid := TodoListFilter{Statuses: []string{"extracted", "observing"}, ActionType: "investigate", Page: 1, PageSize: 20}
	if err := ValidateTodoFilter(valid); err != nil {
		t.Fatalf("ValidateTodoFilter() error = %v", err)
	}
	invalid := valid
	invalid.Statuses = []string{"duplicate"}
	if err := ValidateTodoFilter(invalid); err == nil {
		t.Fatal("ValidateTodoFilter() accepted removed duplicate status")
	}
	invalid = valid
	invalid.PageSize = 101
	if err := ValidateTodoFilter(invalid); err == nil {
		t.Fatal("ValidateTodoFilter() accepted page_size > 100")
	}
}

func TestParseStatuses(t *testing.T) {
	got, err := ParseStatuses(" extracted,observing,extracted ")
	if err != nil {
		t.Fatalf("ParseStatuses() error = %v", err)
	}
	if len(got) != 2 || got[0] != "extracted" || got[1] != "observing" {
		t.Fatalf("ParseStatuses() = %#v", got)
	}
	if _, err := ParseStatuses("extracted,,observing"); err == nil {
		t.Fatal("ParseStatuses() accepted empty status segment")
	}
}

// TestSetTodoStatusRejectsBadInput pins the guards that run before any DB work:
// the entry point is reachable from both the Todo list and M5, so a caller
// without a reason or aiming at a status it does not own must be turned away.
func TestSetTodoStatusRejectsBadInput(t *testing.T) {
	store := &TodoStore{}
	valid := TodoStatusInput{TodoID: 1, Status: "observing", Actor: "principal", Reason: "先留着看"}
	tests := []struct {
		name   string
		mutate func(*TodoStatusInput)
	}{
		{"zero id", func(in *TodoStatusInput) { in.TodoID = 0 }},
		{"blank actor", func(in *TodoStatusInput) { in.Actor = "  " }},
		{"blank reason", func(in *TodoStatusInput) { in.Reason = "  " }},
		// Statuses materialization and execution own are not settable here.
		{"materialized", func(in *TodoStatusInput) { in.Status = "materialized" }},
		{"dropped", func(in *TodoStatusInput) { in.Status = "dropped" }},
		{"unknown", func(in *TodoStatusInput) { in.Status = "parked" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.mutate(&input)
			if _, err := store.SetTodoStatus(context.Background(), input); err == nil {
				t.Fatalf("SetTodoStatus(%+v) accepted invalid input", input)
			}
		})
	}
}

func TestObservableTodoStatusesAreLive(t *testing.T) {
	for status := range observableTodoStatuses {
		if _, ok := activeTodoStatuses[status]; !ok {
			t.Fatalf("settable status %q is not an active Todo status", status)
		}
	}
}

func TestMaterializedTodoCannotReturnToExtracted(t *testing.T) {
	err := validateTodoStatusTransition(1, "materialized", "extracted")
	if err == nil || !strings.Contains(err.Error(), "rerun that Task instead") {
		t.Fatalf("validateTodoStatusTransition() error = %v, want existing Task rejection", err)
	}
}
