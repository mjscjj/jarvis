package extract

import (
	"errors"
	"testing"
)

func TestDecodeExtractionResultRejectsUnknownField(t *testing.T) {
	_, err := DecodeExtractionResult([]byte(`{"candidates":[],"unexpected":true}`))
	if !errors.Is(err, ErrInvalidExtraction) {
		t.Fatalf("DecodeExtractionResult() error = %v", err)
	}
}

func TestValidateCandidateMarksMissingRequiredSlots(t *testing.T) {
	candidate := validCandidate()
	candidate.Slots["change_summary"] = nil
	candidate.InfoSufficient = true
	if err := ValidateCandidate(&candidate); err != nil {
		t.Fatalf("ValidateCandidate() error = %v", err)
	}
	if candidate.InfoSufficient || len(candidate.MissingInfo) != 1 || candidate.MissingInfo[0] != "change_summary" {
		t.Fatalf("candidate after validation = %#v", candidate)
	}
	if _, err := Fingerprint(&candidate, nil); !errors.Is(err, ErrFingerprintIncomplete) {
		t.Fatalf("Fingerprint() error = %v", err)
	}
}

func TestValidateCandidateRejectsInvalidVocabulary(t *testing.T) {
	candidate := validCandidate()
	candidate.Slots["surprise"] = "value"
	if err := ValidateCandidate(&candidate); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("ValidateCandidate() error = %v", err)
	}
}

func TestFingerprintNormalizesTextAndAttendees(t *testing.T) {
	first := validCandidate()
	first.ActionType = "schedule_meeting"
	first.Slots = map[string]any{
		"meeting_title": "  Weekly  SYNC ",
		"attendees":     []string{"OU_B", "ou_a"},
		"proposed_time": "tomorrow",
	}
	second := first
	second.Slots = map[string]any{
		"meeting_title": "weekly sync",
		"attendees":     []any{"ou_a", "ou_b"},
		"proposed_time": "next week",
	}
	projectID := uint64(7)
	a, err := Fingerprint(&first, &projectID)
	if err != nil {
		t.Fatalf("Fingerprint(first) error = %v", err)
	}
	b, err := Fingerprint(&second, &projectID)
	if err != nil {
		t.Fatalf("Fingerprint(second) error = %v", err)
	}
	if a != b {
		t.Fatalf("fingerprints differ: %s != %s", a, b)
	}
}

func validCandidate() Candidate {
	return Candidate{
		ActionType:         "code_change",
		Title:              "修改鉴权",
		Description:        "按讨论修改鉴权逻辑",
		CommitmentStrength: "firm",
		SourceMessageIDs:   []string{"om_1"},
		SourceQuote:        "请修改鉴权逻辑",
		Slots: map[string]any{
			"repo_ref":       "jarvis",
			"change_summary": "修改鉴权",
		},
		InfoSufficient: true,
	}
}
