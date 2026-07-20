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
	prepared, skipped, err := store.prepareResults(context.Background(), batch, []UnitExtraction{{UnitKey: "chat", Candidates: []ResolvedCandidate{resolvedCandidate(candidate)}}})
	if err != nil {
		t.Fatalf("prepareResults() error = %v", err)
	}
	if skipped != 0 {
		t.Fatalf("prepareResults() skipped = %d, want 0", skipped)
	}
	if len(prepared) != 1 || !prepared[0].LeaderAssigned || prepared[0].AssignerOpenID == nil || *prepared[0].AssignerOpenID != "ou_leader" {
		t.Fatalf("prepared = %#v", prepared)
	}
	if prepared[0].Fingerprint == "" || prepared[0].FirstEvidenceAt.IsZero() || prepared[0].LastEvidenceAt.IsZero() {
		t.Fatalf("prepared identity/evidence timestamps = %#v", prepared[0])
	}
}

// target is a required field: a blank target is a hard contract violation and
// must fail fast rather than being silently skipped.
func TestPrepareResultsRejectsBlankTarget(t *testing.T) {
	candidate := strictCandidate()
	candidate.Target = "   "
	store := &PipelineStore{location: time.UTC}
	batch := ChatBatch{
		Group: GroupContext{ID: 3, ChatID: "oc_1"},
		Units: []ConversationUnit{{Key: "chat", Messages: []MessageContext{{
			MessageID: "om_1", Content: "请修改鉴权逻辑", IsNew: true, Extractable: true,
		}}}},
	}
	_, _, err := store.prepareResults(context.Background(), batch, []UnitExtraction{{UnitKey: "chat", Candidates: []ResolvedCandidate{resolvedCandidate(candidate)}}})
	if !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("prepareResults() error = %v", err)
	}
}

func TestPrepareResultsRequiresEveryConversationUnit(t *testing.T) {
	store := &PipelineStore{location: time.UTC}
	batch := ChatBatch{Units: []ConversationUnit{{Key: "chat"}, {Key: "topic:om_root"}}}
	if _, _, err := store.prepareResults(context.Background(), batch, []UnitExtraction{{UnitKey: "chat"}}); err == nil {
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
	return Candidate{
		ActionType: "code_change", Title: "修改鉴权", Target: "jarvis 鉴权逻辑重构",
		Description: "按讨论修改鉴权逻辑", Context: "归属 jarvis 项目，仓库 jarvis",
		OpenQuestions: []string{}, CommitmentStrength: "firm", SourceMessageIDs: []string{"om_1"},
		SourceQuote: "请修改鉴权逻辑",
	}
}

func resolvedCandidate(candidate Candidate) ResolvedCandidate {
	return ResolvedCandidate{Candidate: candidate, Semantic: SemanticResolution{Vector: []float32{1}}}
}
