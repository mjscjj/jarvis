package extract

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"jarvis/internal/contextpack"
)

func TestAnnotationTransportPreservesOpenSemantics(t *testing.T) {
	content := `{"brief":"新的分层内容","scene":"现场","new_concept":{"id":9007199254740993}}`
	for _, wire := range []any{content, json.RawMessage(content)} {
		candidate, _ := json.Marshal(validCandidate())
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(candidate, &fields); err != nil {
			t.Fatal(err)
		}
		fields["annotation"], _ = json.Marshal(wire)
		raw, _ := json.Marshal(map[string]any{"candidates": []any{fields}})
		result, err := DecodeExtractionResult(raw)
		if err != nil {
			t.Fatal(err)
		}
		decoded := result.Candidates[0]
		if string(decoded.Annotation) != content {
			t.Fatalf("content changed: %s", decoded.Annotation)
		}
		source, _ := json.Marshal(decoded)
		packet, err := contextpack.Freeze(source, []byte(`{"messages":[{"message_id":"om_1","content":"原文"}],"project":{"name":"项目"}}`), "default", decoded.Annotation)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"brief":"新的分层内容"`, `"new_concept":{"id":9007199254740993}`} {
			if !strings.Contains(string(packet), want) {
				t.Fatalf("lost semantics: %s", packet)
			}
		}
	}
	for _, content := range []any{"not JSON", "[]", "null", "", []any{1}, nil} {
		raw, _ := json.Marshal(map[string]any{"annotation": content})
		var candidate Candidate
		if err := json.Unmarshal(raw, &candidate); err == nil {
			t.Fatalf("accepted invalid content: %s", raw)
		}
	}
}

func TestOutputContractExamplesDecode(t *testing.T) {
	start := strings.Index(outputContract, "{\n  \"candidates\"")
	end := strings.LastIndex(outputContract, "\n}") + 2
	if start < 0 || end <= start {
		t.Fatal("missing output example")
	}
	result, err := DecodeExtractionResult([]byte(outputContract[start:end]))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range result.Candidates {
		if len(candidate.Annotation) == 0 {
			t.Fatal("example missing content")
		}
	}
}

// TestDecodeExtractionResultToleratesPresentationNoise pins that the shapes a
// model gets wrong without losing meaning — an invented key, a markdown fence,
// prose after the object — do not discard a whole unit of candidates.
func TestDecodeExtractionResultToleratesPresentationNoise(t *testing.T) {
	body := `{"candidates":[{"action_type":"Reply Message","status":"extracted","title":"回复张伟",` +
		`"target":"张伟的排期问题","project_hint":"","source_message_ids":["om_1"],"trigger_message_id":"om_1",` +
		`"source_quote":"这个排期能确认下吗","payload":"张伟在等我确认排期。","description":"多余字段"}]}`
	for name, payload := range map[string]string{
		"unknown field": body,
		"code fence":    "```json\n" + body + "\n```",
		"trailing prose": body + `

以上就是本轮抽取到的线索。`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := DecodeExtractionResult([]byte(payload))
			if err != nil {
				t.Fatalf("DecodeExtractionResult() error = %v", err)
			}
			if len(result.Candidates) != 1 {
				t.Fatalf("candidates = %d, want 1", len(result.Candidates))
			}
			// action_type is normalized rather than rejected, so casing and
			// separators cannot split the dedup fingerprint.
			if got := result.Candidates[0].ActionType; got != "reply_message" {
				t.Fatalf("action_type = %q, want reply_message", got)
			}
		})
	}
}

// TestDecodeExtractionResultRejectsMissingCandidates pins what is still an
// error: a final message that does not carry the field we consume. The worker
// feeds this back to the model and re-extracts.
func TestDecodeExtractionResultRejectsMissingCandidates(t *testing.T) {
	for name, payload := range map[string]string{
		"prose":              `本轮没有发现值得处理的线索。`,
		"missing candidates": `{"result":"ok"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeExtractionResult([]byte(payload)); !errors.Is(err, ErrInvalidExtraction) {
				t.Fatalf("DecodeExtractionResult() error = %v", err)
			}
		})
	}
}

func TestValidateCandidateRejectsBlankTarget(t *testing.T) {
	candidate := validCandidate()
	candidate.Target = "   "
	if err := ValidateCandidate(&candidate); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("ValidateCandidate() error = %v", err)
	}
}

func TestValidateCandidateRejectsBlankPayload(t *testing.T) {
	candidate := validCandidate()
	candidate.Payload = "   "
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

// TestFingerprintIgnoresPayload keeps dedup identity on
// (action_type, project_id, target). Rewording model semantics must not
// resurface the same clue as a second Todo.
func TestFingerprintIgnoresPayload(t *testing.T) {
	first := validCandidate()
	second := first
	second.Payload = `{"desired_outcome":"换一种说法","blocker":"等对方回复"}`
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

// TestDecodeExtractionResultKeepsPayloadVerbatim pins the open pocket: Go
// must not parse, normalize or drop whatever the model wrote there.
func TestDecodeExtractionResultKeepsPayloadVerbatim(t *testing.T) {
	// payload is declared as a string in the provider schema, so the model
	// sends JSON as text; decoding must hand it back untouched.
	payload := `{"candidates":[{"action_type":"manual_followup","status":"extracted","title":"会后处理","target":"公会基建Agent 日会",` +
		`"project_hint":null,"source_message_ids":["vc_meeting_1"],"trigger_message_id":"vc_meeting_1","source_quote":"采集结果：permission_denied",` +
		`"payload":"{\"desired_outcome\":\"产出结论并生成我的待办\",\"blocker\":\"妙记无 view 权限\",\"next\":\"申请后重读\"}"}]}`
	result, err := DecodeExtractionResult([]byte(payload))
	if err != nil {
		t.Fatalf("DecodeExtractionResult() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(result.Candidates))
	}
	got := result.Candidates[0]
	if got.Payload != `{"desired_outcome":"产出结论并生成我的待办","blocker":"妙记无 view 权限","next":"申请后重读"}` {
		t.Fatalf("payload = %q", got.Payload)
	}
}

func validCandidate() Candidate {
	return Candidate{
		ActionType:       "code_change",
		Status:           "extracted",
		Title:            "修改鉴权",
		Target:           "jarvis 鉴权逻辑重构",
		Payload:          "最终完成鉴权重构并合入主干；归属 jarvis 项目，仓库 jarvis。",
		SourceMessageIDs: []string{"om_1"}, TriggerMessageID: "om_1",
		SourceQuote: "请修改鉴权逻辑",
	}
}

func TestCandidatePreservesUnknownSemanticFields(t *testing.T) {
	candidate := strictCandidate()
	raw, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["new_semantics"] = json.RawMessage(`{"id":9007199254740993,"reason":["one","two"]}`)
	fields["annotation"] = json.RawMessage(`{"brief":"摘要","scene":"现场","unfamiliar":true}`)
	raw, err = json.Marshal(map[string]any{"candidates": []any{fields}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := DecodeExtractionResult(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.Candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) || !strings.Contains(string(encoded), `"unfamiliar":true`) {
		t.Fatalf("semantic fields lost: %s", encoded)
	}
}
