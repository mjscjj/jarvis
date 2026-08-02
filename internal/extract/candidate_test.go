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

func TestValidateCandidateRejectsBlankDesiredOutcome(t *testing.T) {
	candidate := validCandidate()
	candidate.DesiredOutcome = "   "
	if err := ValidateCandidate(&candidate); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("ValidateCandidate() error = %v", err)
	}
}

// TestFingerprintIgnoresDesiredOutcomeAndSemantics keeps dedup identity on
// (action_type, project_id, target). A clue whose completion criterion or
// free-form reasoning is reworded must not resurface as a second Todo.
func TestFingerprintIgnoresDesiredOutcomeAndSemantics(t *testing.T) {
	first := validCandidate()
	second := first
	second.DesiredOutcome = "换一种说法描述同一个完成状态"
	second.Semantics = `{"blocker":"等对方回复"}`
	a, err := Fingerprint(&first, nil)
	if err != nil {
		t.Fatalf("Fingerprint(first) error = %v", err)
	}
	b, err := Fingerprint(&second, nil)
	if err != nil {
		t.Fatalf("Fingerprint(second) error = %v", err)
	}
	if a != b {
		t.Fatalf("fingerprints differ: %s != %s", a, b)
	}
}

// TestDecodeExtractionResultKeepsSemanticsVerbatim pins the open pocket: Go
// must not parse, normalize or drop whatever the model wrote there.
func TestDecodeExtractionResultKeepsSemanticsVerbatim(t *testing.T) {
	// semantics is declared as a string in the provider schema, so the model
	// sends JSON as text; decoding must hand it back untouched.
	payload := `{"candidates":[{"action_type":"manual_followup","status":"extracted","title":"会后处理","target":"公会基建Agent 日会",` +
		`"desired_outcome":"产出结论并生成我的待办","description":"当前卡在妙记无权限","context":"",` +
		`"open_questions":[],"commitment_strength":"firm","assigner_open_id":null,"project_hint":null,` +
		`"due_date":null,"source_message_ids":["vc_meeting_1"],"source_quote":"采集结果：permission_denied",` +
		`"semantics":"{\"blocker\":\"妙记无 view 权限\",\"next\":\"申请后重读\"}"}]}`
	result, err := DecodeExtractionResult([]byte(payload))
	if err != nil {
		t.Fatalf("DecodeExtractionResult() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(result.Candidates))
	}
	got := result.Candidates[0]
	if got.DesiredOutcome != "产出结论并生成我的待办" {
		t.Fatalf("desired_outcome = %q", got.DesiredOutcome)
	}
	if got.Semantics != `{"blocker":"妙记无 view 权限","next":"申请后重读"}` {
		t.Fatalf("semantics = %q", got.Semantics)
	}
}

func validCandidate() Candidate {
	return Candidate{
		ActionType:         "code_change",
		Status:             "extracted",
		Title:              "修改鉴权",
		Target:             "jarvis 鉴权逻辑重构",
		DesiredOutcome:     "鉴权逻辑按讨论改完并合入主干",
		Description:        "按讨论修改鉴权逻辑",
		Context:            "归属 jarvis 项目，仓库 jarvis",
		OpenQuestions:      []string{},
		CommitmentStrength: "firm",
		SourceMessageIDs:   []string{"om_1"},
		SourceQuote:        "请修改鉴权逻辑",
	}
}
