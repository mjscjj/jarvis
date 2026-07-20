package execute

import (
	"errors"
	"testing"
)

func TestValidateTaskFilter(t *testing.T) {
	if err := ValidateTaskFilter(TaskFilter{Statuses: []string{"pending", "executing", "done"}, Page: 1, PageSize: 20}); err != nil {
		t.Fatalf("ValidateTaskFilter() error = %v", err)
	}
	for _, filter := range []TaskFilter{
		{Page: 0, PageSize: 20},
		{Page: 1, PageSize: 101},
		{Statuses: []string{"nonsense"}, Page: 1, PageSize: 20},
	} {
		if err := ValidateTaskFilter(filter); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("ValidateTaskFilter(%#v) error = %v", filter, err)
		}
	}
}

func TestParseStatuses(t *testing.T) {
	statuses, err := ParseStatuses("pending,done,pending")
	if err != nil {
		t.Fatalf("ParseStatuses() error = %v", err)
	}
	if len(statuses) != 2 || statuses[0] != "pending" || statuses[1] != "done" {
		t.Fatalf("statuses = %v", statuses)
	}
	if _, err := ParseStatuses("pending,unknown"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ParseStatuses() error = %v", err)
	}
}

// TestAwaitingApprovalStatusAllowed guards that the new gate status is a valid
// filter/query value everywhere Tasks are listed.
func TestAwaitingApprovalStatusAllowed(t *testing.T) {
	if err := ValidateTaskFilter(TaskFilter{Statuses: []string{"awaiting_approval"}, Page: 1, PageSize: 20}); err != nil {
		t.Fatalf("awaiting_approval must be a valid filter status: %v", err)
	}
	statuses, err := ParseStatuses("awaiting_approval")
	if err != nil || len(statuses) != 1 || statuses[0] != "awaiting_approval" {
		t.Fatalf("ParseStatuses(awaiting_approval) = %v, err = %v", statuses, err)
	}
}

func TestCanonicalJSONObject(t *testing.T) {
	result, err := canonicalJSONObject([]byte(`{"summary":"done","count":1}`))
	if err != nil {
		t.Fatalf("canonicalJSONObject() error = %v", err)
	}
	if string(result) != `{"count":1,"summary":"done"}` {
		t.Fatalf("result = %s", result)
	}
	for _, raw := range [][]byte{nil, []byte(`[]`), []byte(`{}`), []byte(`{} {}`)} {
		if _, err := canonicalJSONObject(raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("canonicalJSONObject(%s) error = %v", raw, err)
		}
	}
}

func TestNewStoreRejectsNilDB(t *testing.T) {
	if _, err := NewStore(nil); err == nil {
		t.Fatal("NewStore(nil) succeeded")
	}
}
