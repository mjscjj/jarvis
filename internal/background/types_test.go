package background

import (
	"encoding/json"
	"testing"
)

func validProjectInput() ProjectInput {
	return ProjectInput{Name: "Jarvis", Role: "owner", Status: "active", Priority: 3}
}

func validPersonUpdateInput() PersonUpdateInput {
	return PersonUpdateInput{Name: "Leader", Role: "leader", PriorityWeight: 0.9}
}

func validPersonCreateInput() PersonCreateInput {
	return PersonCreateInput{OpenID: "ou_abc", PersonUpdateInput: validPersonUpdateInput()}
}

func TestProjectInputValidate(t *testing.T) {
	t.Parallel()
	base := validProjectInput()
	if err := base.validate(); err != nil {
		t.Fatalf("valid project rejected: %v", err)
	}

	cases := map[string]func(*ProjectInput){
		"blank name":     func(in *ProjectInput) { in.Name = "  " },
		"bad role":       func(in *ProjectInput) { in.Role = "boss" },
		"bad status":     func(in *ProjectInput) { in.Status = "running" },
		"priority zero":  func(in *ProjectInput) { in.Priority = 0 },
		"priority high":  func(in *ProjectInput) { in.Priority = 6 },
		"blank code":     func(in *ProjectInput) { blank := "  "; in.Code = &blank },
		"bad repos json": func(in *ProjectInput) { in.Repos = json.RawMessage("{oops") },
	}
	for name, mutate := range cases {
		in := validProjectInput()
		mutate(&in)
		if err := in.validate(); err == nil {
			t.Errorf("case %q: expected validation error, got nil", name)
		}
	}
}

func TestProjectInputValidateAcceptsJSON(t *testing.T) {
	t.Parallel()
	in := validProjectInput()
	in.Repos = json.RawMessage(`["a","b"]`)
	in.Timeline = json.RawMessage(`{"kickoff":"2026-01-01"}`)
	if err := in.validate(); err != nil {
		t.Fatalf("valid JSON columns rejected: %v", err)
	}
}

func TestPersonCreateInputValidate(t *testing.T) {
	t.Parallel()
	base := validPersonCreateInput()
	if err := base.validate(); err != nil {
		t.Fatalf("valid person create rejected: %v", err)
	}

	cases := map[string]func(*PersonCreateInput){
		"blank open_id":   func(in *PersonCreateInput) { in.OpenID = "" },
		"blank name":      func(in *PersonCreateInput) { in.Name = " " },
		"bad role":        func(in *PersonCreateInput) { in.Role = "manager" },
		"weight negative": func(in *PersonCreateInput) { in.PriorityWeight = -0.1 },
		"weight over one": func(in *PersonCreateInput) { in.PriorityWeight = 1.5 },
	}
	for name, mutate := range cases {
		in := validPersonCreateInput()
		mutate(&in)
		if err := in.validate(); err == nil {
			t.Errorf("case %q: expected validation error, got nil", name)
		}
	}
}

func TestPersonUpdateInputDoesNotRequireIdentity(t *testing.T) {
	t.Parallel()
	in := validPersonUpdateInput()
	if err := in.validate(); err != nil {
		t.Fatalf("valid person update rejected: %v", err)
	}
}

func TestListFilterValidate(t *testing.T) {
	t.Parallel()
	if err := (ListFilter{Page: 1, PageSize: 20}).validate(); err != nil {
		t.Fatalf("valid filter rejected: %v", err)
	}
	for name, filter := range map[string]ListFilter{
		"page zero":     {Page: 0, PageSize: 20},
		"size zero":     {Page: 1, PageSize: 0},
		"size over max": {Page: 1, PageSize: maxPageSize + 1},
	} {
		if err := filter.validate(); err == nil {
			t.Errorf("case %q: expected error, got nil", name)
		}
	}
}

func TestListFilterOffset(t *testing.T) {
	t.Parallel()
	if got := (ListFilter{Page: 3, PageSize: 20}).offset(); got != 40 {
		t.Fatalf("offset = %d, want 40", got)
	}
}

func TestValidateOptionalJSON(t *testing.T) {
	t.Parallel()
	if err := validateOptionalJSON("repos", nil); err != nil {
		t.Fatalf("nil JSON rejected: %v", err)
	}
	if err := validateOptionalJSON("repos", json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}
	if err := validateOptionalJSON("repos", json.RawMessage("not json")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
