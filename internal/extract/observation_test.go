package extract

import (
	"errors"
	"testing"
)

func validObservation() ObservationCandidate {
	return ObservationCandidate{
		Subject:          "Bax PC 评测范围",
		Content:          "群里定了 Bax PC 评测只看看板，问答类 case 不纳入这一轮。",
		SourceMessageIDs: []string{"om_1"},
		SourceQuote:      "这轮只看看板",
	}
}

func TestValidateObservationAcceptsMinimalFacts(t *testing.T) {
	t.Parallel()
	observation := validObservation()
	if err := ValidateObservation(&observation); err != nil {
		t.Fatalf("ValidateObservation() error = %v", err)
	}
}

func TestValidateObservationRejectsBlankFields(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*ObservationCandidate){
		"subject": func(o *ObservationCandidate) { o.Subject = "   " },
		"content": func(o *ObservationCandidate) { o.Content = "" },
		"quote":   func(o *ObservationCandidate) { o.SourceQuote = "  " },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			observation := validObservation()
			mutate(&observation)
			if err := ValidateObservation(&observation); !errors.Is(err, ErrInvalidObservation) {
				t.Fatalf("error = %v, want ErrInvalidObservation", err)
			}
		})
	}
}

func TestValidateObservationRequiresEvidence(t *testing.T) {
	t.Parallel()
	observation := validObservation()
	observation.SourceMessageIDs = nil
	if err := ValidateObservation(&observation); !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("error = %v, want ErrInvalidObservation", err)
	}
}

// Re-scanning the same message must not create a second row.
func TestObservationDedupKeyIsStable(t *testing.T) {
	t.Parallel()
	first := validObservation()
	second := validObservation()
	second.Subject = "  Bax PC 评测范围  " // whitespace only

	firstKey, err := ObservationDedupKey(&first, nil)
	if err != nil {
		t.Fatalf("ObservationDedupKey() error = %v", err)
	}
	secondKey, err := ObservationDedupKey(&second, nil)
	if err != nil {
		t.Fatalf("ObservationDedupKey() error = %v", err)
	}
	if firstKey != secondKey {
		t.Fatalf("keys differ for the same fact: %s vs %s", firstKey, secondKey)
	}
}

// One message can legitimately yield several distinct observations, so content
// has to be part of the identity — otherwise the second one would be silently
// swallowed as a duplicate.
func TestObservationDedupKeySeparatesDistinctFacts(t *testing.T) {
	t.Parallel()
	first := validObservation()
	second := validObservation()
	second.Content = "群里同时提到问答类 case 下一轮再补。"

	firstKey, err := ObservationDedupKey(&first, nil)
	if err != nil {
		t.Fatalf("ObservationDedupKey() error = %v", err)
	}
	secondKey, err := ObservationDedupKey(&second, nil)
	if err != nil {
		t.Fatalf("ObservationDedupKey() error = %v", err)
	}
	if firstKey == secondKey {
		t.Fatalf("two different facts from one message collapsed onto key %s", firstKey)
	}
}

func TestObservationDedupKeySeparatesProjects(t *testing.T) {
	t.Parallel()
	observation := validObservation()
	projectA := uint64(1)
	projectB := uint64(2)

	keyA, err := ObservationDedupKey(&observation, &projectA)
	if err != nil {
		t.Fatalf("ObservationDedupKey() error = %v", err)
	}
	keyB, err := ObservationDedupKey(&observation, &projectB)
	if err != nil {
		t.Fatalf("ObservationDedupKey() error = %v", err)
	}
	if keyA == keyB {
		t.Fatalf("same fact under two projects collapsed onto key %s", keyA)
	}
}

func TestDecodeExtractionResultReadsObservations(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"candidates":[],"observations":[{
		"subject":"Bax PC 评测范围",
		"content":"群里定了这轮只看看板。",
		"project_hint":null,
		"source_message_ids":["om_1"],
		"source_quote":"这轮只看看板"
	}]}`)
	result, err := DecodeExtractionResult(payload)
	if err != nil {
		t.Fatalf("DecodeExtractionResult() error = %v", err)
	}
	if len(result.Observations) != 1 || result.Observations[0].Subject != "Bax PC 评测范围" {
		t.Fatalf("observations = %#v", result.Observations)
	}
}

// A batch with nothing worth remembering is a normal outcome, not a contract
// violation.
func TestDecodeExtractionResultAllowsMissingObservations(t *testing.T) {
	t.Parallel()
	result, err := DecodeExtractionResult([]byte(`{"candidates":[]}`))
	if err != nil {
		t.Fatalf("DecodeExtractionResult() error = %v", err)
	}
	if len(result.Observations) != 0 {
		t.Fatalf("observations = %#v, want empty", result.Observations)
	}
}

func TestDecodeExtractionResultRejectsInvalidObservation(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"candidates":[],"observations":[{
		"subject":"",
		"content":"内容在，但主题空了。",
		"project_hint":null,
		"source_message_ids":["om_1"],
		"source_quote":"内容在"
	}]}`)
	if _, err := DecodeExtractionResult(payload); !errors.Is(err, ErrInvalidExtraction) {
		t.Fatalf("error = %v, want ErrInvalidExtraction", err)
	}
}
