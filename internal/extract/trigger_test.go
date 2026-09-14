package extract

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"jarvis/internal/contextpack"
	"jarvis/internal/domain"
)

func TestOptionalTriggerLinkFrozenIndependentlyOfQuote(t *testing.T) {
	const link = "https://applink.feishu.cn/client/chat/open?openChatId=oc_group&position=42"
	message := messageContext(&domain.Message{MessageID: "om_trigger", Content: "原始交办", SenderType: "user", RenderOK: true, SourceURL: stringPointerForTrigger(link)}, false)
	unit := ConversationUnit{Messages: []MessageContext{
		{MessageID: "om_other", Content: "请处理", IsNew: true, Extractable: true, SourceURL: "https://example.com/other"},
		message,
	}}
	candidate := validCandidate()
	candidate.SourceMessageIDs = []string{"om_other", "om_trigger"}
	candidate.TriggerMessageID = "om_trigger"
	candidate.SourceQuote = "请处理"
	if err := ValidateCandidate(&candidate); err != nil {
		t.Fatal(err)
	}
	if err := validateCandidateEvidence(unit, &candidate); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (&PipelineStore{}).buildContextSnapshot(t.Context(), ChatBatch{}, unit, candidate, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := snapshot.Encode()
	if err != nil {
		t.Fatal(err)
	}
	source, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := contextpack.Freeze(source, capture, candidate.Payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := contextpack.SourceURL(frozen); got != link {
		t.Fatalf("frozen source URL = %q", got)
	}

}

func stringPointerForTrigger(value string) *string { return &value }

// Optional presentation metadata must not reject, retry or suppress an otherwise
// valid candidate, including through decoding and creation-time freezing.
func TestOptionalTriggerDoesNotGateExtraction(t *testing.T) {
	for _, wire := range []string{"omitted", "null", `""`, `"om_unknown"`} {
		t.Run(wire, func(t *testing.T) {
			original := retryCandidate("当前服务和架构梳理")
			raw, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, "trigger_message_id")
			if wire != "omitted" {
				fields["trigger_message_id"] = json.RawMessage(wire)
			}
			raw, err = json.Marshal(map[string]any{"candidates": []any{fields}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := DecodeExtractionResult(raw)
			if err != nil {
				t.Fatal(err)
			}
			candidate := result.Candidates[0]
			batch := retryBatch()
			prepared, err := (&PipelineStore{location: time.UTC}).prepareCandidate(t.Context(), batch, batch.Units[0], candidate)
			if err != nil {
				t.Fatal(err)
			}
			if got := contextpack.SourceURL(prepared.Content); got != "" {
				t.Fatalf("unexpected source URL: %q", got)
			}
			store := &fakePipelineStore{batches: []ChatBatch{batch}}
			model := &fakeModelExtractor{result: result}
			worker, err := NewWorker(store, model, &fakeCandidateDeduplicator{}, &fakeToolBoxBuilder{}, validWorkerOptions())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := extractAllChats(t.Context(), worker); err != nil {
				t.Fatal(err)
			}
			if len(model.prompts) != 1 || store.persistCalls != 1 {
				t.Fatalf("model calls=%d persists=%d", len(model.prompts), store.persistCalls)
			}
			candidate.SourceQuote = "不存在的引文"
			if err := validateCandidateEvidence(batch.Units[0], &candidate); !errors.Is(err, ErrEvidenceQuoteMismatch) {
				t.Fatalf("original evidence validation changed: %v", err)
			}
		})
	}
}
