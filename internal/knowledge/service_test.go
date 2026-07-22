package knowledge

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPrepareCreateEntityFact(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 7, 22, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	input := CreateInput{
		Subject:       EntityRef{Type: EntityPerson, ID: 7},
		Predicate:     " REPORTS_TO ",
		Object:        &EntityRef{Type: EntityPerson, ID: 8},
		AssertionKind: "MANUAL",
		ValidFrom:     &from,
		SourceType:    " feishu_message ",
		SourceID:      "om_123",
	}

	prepared, err := prepareCreate(input)
	if err != nil {
		t.Fatalf("prepareCreate() error = %v", err)
	}
	if prepared.input.Predicate != "reports_to" {
		t.Errorf("predicate = %q, want reports_to", prepared.input.Predicate)
	}
	if prepared.input.AssertionKind != "manual" {
		t.Errorf("assertion kind = %q, want manual", prepared.input.AssertionKind)
	}
	if prepared.input.SourceType != "feishu_message" {
		t.Errorf("source type = %q, want feishu_message", prepared.input.SourceType)
	}
	if prepared.input.ValidFrom == nil || !prepared.input.ValidFrom.Equal(from) {
		t.Errorf("valid_from = %v, want %v", prepared.input.ValidFrom, from)
	}
	if prepared.dedupKey == "" {
		t.Fatal("dedup key is empty")
	}
}

func TestPrepareCreateCanonicalValueFact(t *testing.T) {
	t.Parallel()
	input := CreateInput{
		Subject:       EntityRef{Type: EntityProject, ID: 1},
		Predicate:     "uses",
		Value:         json.RawMessage(`{ "name": "MySQL", "kind": "storage" }`),
		AssertionKind: "system",
		SourceType:    "migration",
		SourceID:      "bootstrap-1",
	}

	prepared, err := prepareCreate(input)
	if err != nil {
		t.Fatalf("prepareCreate() error = %v", err)
	}
	if got, want := string(prepared.value), `{"kind":"storage","name":"MySQL"}`; got != want {
		t.Errorf("canonical value = %s, want %s", got, want)
	}
}

func TestPrepareCreateRejectsTypedPredicate(t *testing.T) {
	t.Parallel()
	_, err := prepareCreate(CreateInput{
		Subject:       EntityRef{Type: EntityTask, ID: 1},
		Predicate:     "belongs_to_project",
		Object:        &EntityRef{Type: EntityProject, ID: 2},
		AssertionKind: "system",
		SourceType:    "test",
		SourceID:      "typed-edge",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("prepareCreate() error = %v, want ErrInvalidInput", err)
	}
}

func TestPrepareCreateRequiresInferenceEvidence(t *testing.T) {
	t.Parallel()
	confidence := 0.8
	_, err := prepareCreate(CreateInput{
		Subject:       EntityRef{Type: EntityPerson, ID: 1},
		Predicate:     "prefers",
		Value:         json.RawMessage(`"concise updates"`),
		AssertionKind: "inferred",
		Confidence:    &confidence,
		SourceType:    "feishu_message",
		SourceID:      "om_1",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("prepareCreate() error = %v, want ErrInvalidInput", err)
	}
}

func TestPrepareCreateRequiresExactlyOneTarget(t *testing.T) {
	t.Parallel()
	_, err := prepareCreate(CreateInput{
		Subject:       EntityRef{Type: EntityProject, ID: 1},
		Predicate:     "depends_on",
		Object:        &EntityRef{Type: EntityProject, ID: 2},
		Value:         json.RawMessage(`"project-2"`),
		AssertionKind: "manual",
		SourceType:    "user",
		SourceID:      "request-1",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("prepareCreate() error = %v, want ErrInvalidInput", err)
	}
}

func TestValidateFilterRequiresCompleteEntityReference(t *testing.T) {
	t.Parallel()
	typeName := EntityProject
	err := validateFilter(FactFilter{SubjectType: &typeName, Page: 1, PageSize: 20})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("validateFilter() error = %v, want ErrInvalidInput", err)
	}
}
