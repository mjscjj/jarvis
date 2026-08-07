package extract

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateCandidateEvidenceRejectsContextOnlySource(t *testing.T) {
	unit := contractChatBatch().Units[0]
	candidate := contractStrictCandidate()
	candidate.SourceMessageIDs = []string{"om_context"}
	candidate.SourceQuote = "earlier context"
	err := validateCandidateEvidence(unit, &candidate)
	if !errors.Is(err, ErrEvidenceNoNewSource) {
		t.Fatalf("validateCandidateEvidence() error = %v, want ErrEvidenceNoNewSource", err)
	}
	if !selfCorrectableEvidence(err) {
		t.Fatalf("context-only source should be self-correctable, got %v", err)
	}
}

func TestBuildPromptDropsContextBeforeNewEvidence(t *testing.T) {
	batch := contractChatBatch()
	unit := batch.Units[0]
	unit.Messages[0].Content = "old-marker " + strings.Repeat("x", 5000)
	prompt, err := BuildPrompt(batch, unit, nil, time.Now(), PromptOptions{SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 4000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if strings.Contains(prompt.User, "old-marker") || !strings.Contains(prompt.User, "new request") {
		t.Fatalf("prompt = %q", prompt.User)
	}
}

// A candidate with a blank target has no dedup identity and must fail fast.
func TestPrepareResultsRejectsBlankTargetContract(t *testing.T) {
	store := &PipelineStore{location: time.UTC}
	batch := contractChatBatch()
	candidate := contractStrictCandidate()
	candidate.Target = ""
	_, _, err := store.prepareResults(context.Background(), batch, []UnitExtraction{{UnitKey: "chat", Candidates: []ResolvedCandidate{resolvedCandidate(candidate)}}})
	if !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("prepareResults() error = %v", err)
	}
}

func TestPrepareResultsDerivesLeaderAssigner(t *testing.T) {
	store := &PipelineStore{location: time.UTC}
	batch := contractChatBatch()
	prepared, _, err := store.prepareResults(context.Background(), batch, []UnitExtraction{{UnitKey: "chat", Candidates: []ResolvedCandidate{resolvedCandidate(contractStrictCandidate())}}})
	if err != nil {
		t.Fatalf("prepareResults() error = %v", err)
	}
	if len(prepared) != 1 || !prepared[0].LeaderAssigned || prepared[0].AssignerOpenID == nil || *prepared[0].AssignerOpenID != "ou_leader" {
		t.Fatalf("prepared = %#v", prepared)
	}
}

func contractChatBatch() ChatBatch {
	projectID := uint64(9)
	contextMessage := MessageContext{
		DatabaseID: 1, MessageID: "om_context", ChatID: "oc_chat", SenderOpenID: "ou_peer",
		SenderName: "Peer", Content: "earlier context", CreateTime: 1000, Extractable: true,
	}
	newMessage := MessageContext{
		DatabaseID: 2, MessageID: "om_new", ChatID: "oc_chat", SenderOpenID: "ou_leader",
		SenderName: "Leader", Content: "new request: modify auth", CreateTime: 2000,
		IsNew: true, IsLeader: true, Extractable: true,
	}
	return ChatBatch{
		Group:   GroupContext{ID: 3, ChatID: "oc_chat", Name: "work", ProjectID: &projectID},
		Project: &ProjectContext{ID: projectID, Name: "Jarvis"},
		Units: []ConversationUnit{{
			Key: "chat", Messages: []MessageContext{contextMessage, newMessage},
			Participants: []ParticipantContext{{OpenID: "ou_leader", Name: "Leader", Role: "leader", IsLeader: true}},
		}},
		LastNew: newMessage,
	}
}

func contractStrictCandidate() Candidate {
	return Candidate{
		ActionType: "code_change", Status: "extracted", Title: "Modify auth", Target: "jarvis auth refactor",
		Payload:          "Implement the requested auth change in repo jarvis and merge it.",
		SourceMessageIDs: []string{"om_new"}, SourceQuote: "new request: modify auth",
	}
}
