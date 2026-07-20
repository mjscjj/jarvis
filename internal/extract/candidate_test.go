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

func TestValidateCandidateRejectsBlankTarget(t *testing.T) {
	candidate := validCandidate()
	candidate.Target = "   "
	if err := ValidateCandidate(&candidate); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("ValidateCandidate() error = %v", err)
	}
}

func TestValidateCandidateRejectsBlankOpenQuestion(t *testing.T) {
	candidate := validCandidate()
	candidate.OpenQuestions = []string{"具体时间?", "  "}
	if err := ValidateCandidate(&candidate); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("ValidateCandidate() error = %v", err)
	}
}

func TestFingerprintRejectsBlankTarget(t *testing.T) {
	candidate := validCandidate()
	candidate.Target = ""
	// target is a required field, so validation rejects a blank target before the
	// fingerprint identity check would.
	if _, err := Fingerprint(&candidate, nil); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("Fingerprint() error = %v", err)
	}
}

func TestFingerprintNormalizesTarget(t *testing.T) {
	first := validCandidate()
	first.ActionType = "schedule_meeting"
	first.Target = "  Weekly  SYNC  会议 "
	second := first
	second.Target = "weekly sync 会议"
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
		Target:             "jarvis 鉴权逻辑重构",
		Description:        "按讨论修改鉴权逻辑",
		Context:            "归属 jarvis 项目，仓库 jarvis",
		OpenQuestions:      []string{},
		CommitmentStrength: "firm",
		SourceMessageIDs:   []string{"om_1"},
		SourceQuote:        "请修改鉴权逻辑",
	}
}
