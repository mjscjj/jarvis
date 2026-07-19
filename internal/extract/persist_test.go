package extract

import (
	"context"
	"errors"
	"testing"
	"time"

	"jarvis/internal/domain"
)

func TestPrepareResultsBindsLeaderEvidence(t *testing.T) {
	candidate := strictCandidate()
	store := &PipelineStore{location: time.UTC}
	batch := ChatBatch{
		Group: GroupContext{ID: 3, ChatID: "oc_1"},
		Units: []ConversationUnit{{
			Key: "chat",
			Messages: []MessageContext{{
				MessageID: "om_1", SenderOpenID: "ou_leader", Content: "请修改鉴权逻辑",
				CreateTime: 1_700_000_000_000, IsNew: true, IsLeader: true, Extractable: true,
			}},
			Participants: []ParticipantContext{{OpenID: "ou_leader", Role: "leader", IsLeader: true}},
		}},
	}
	prepared, err := store.prepareResults(context.Background(), batch, []UnitExtraction{{UnitKey: "chat", Candidates: []ResolvedCandidate{resolvedCandidate(candidate)}}})
	if err != nil {
		t.Fatalf("prepareResults() error = %v", err)
	}
	if len(prepared) != 1 || !prepared[0].LeaderAssigned || prepared[0].AssignerOpenID == nil || *prepared[0].AssignerOpenID != "ou_leader" {
		t.Fatalf("prepared = %#v", prepared)
	}
	if prepared[0].Fingerprint == "" || prepared[0].FirstEvidenceAt.IsZero() || prepared[0].LastEvidenceAt.IsZero() {
		t.Fatalf("prepared identity/evidence timestamps = %#v", prepared[0])
	}
}

func TestPrepareResultsRejectsIncompleteIdentity(t *testing.T) {
	candidate := strictCandidate()
	candidate.Slots["change_summary"] = nil
	store := &PipelineStore{location: time.UTC}
	batch := ChatBatch{
		Group: GroupContext{ID: 3, ChatID: "oc_1"},
		Units: []ConversationUnit{{Key: "chat", Messages: []MessageContext{{
			MessageID: "om_1", Content: "请修改鉴权逻辑", IsNew: true, Extractable: true,
		}}}},
	}
	_, err := store.prepareResults(context.Background(), batch, []UnitExtraction{{UnitKey: "chat", Candidates: []ResolvedCandidate{resolvedCandidate(candidate)}}})
	if !errors.Is(err, ErrFingerprintIncomplete) {
		t.Fatalf("prepareResults() error = %v", err)
	}
}

func TestPrepareResultsRequiresEveryConversationUnit(t *testing.T) {
	store := &PipelineStore{location: time.UTC}
	batch := ChatBatch{Units: []ConversationUnit{{Key: "chat"}, {Key: "topic:om_root"}}}
	if _, err := store.prepareResults(context.Background(), batch, []UnitExtraction{{UnitKey: "chat"}}); err == nil {
		t.Fatal("prepareResults() accepted missing conversation unit result")
	}
}

func TestExtractableMessage(t *testing.T) {
	if extractableMessage(nilMessage("bot", "请处理", true)) {
		t.Fatal("extractableMessage() accepted a bot message")
	}
	if extractableMessage(nilMessage("user", "[图片]", true)) {
		t.Fatal("extractableMessage() accepted image placeholder")
	}
	if !extractableMessage(nilMessage("user", "请处理 123", true)) {
		t.Fatal("extractableMessage() rejected normal text")
	}
}

func nilMessage(senderType, content string, renderOK bool) *domain.Message {
	return &domain.Message{SenderType: senderType, Content: content, RenderOK: renderOK}
}

func strictCandidate() Candidate {
	slots := make(map[string]any, len(allowedSlots))
	for name := range allowedSlots {
		slots[name] = nil
	}
	slots["repo_ref"] = "jarvis"
	slots["change_summary"] = "修改鉴权"
	return Candidate{
		ActionType: "code_change", Title: "修改鉴权", Description: "按讨论修改鉴权逻辑",
		CommitmentStrength: "firm", SourceMessageIDs: []string{"om_1"},
		SourceQuote: "请修改鉴权逻辑", Slots: slots, InfoSufficient: true, MissingInfo: []string{},
	}
}

func resolvedCandidate(candidate Candidate) ResolvedCandidate {
	return ResolvedCandidate{Candidate: candidate, Semantic: SemanticResolution{Vector: []float32{1}}}
}
