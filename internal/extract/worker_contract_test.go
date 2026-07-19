package extract

import (
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
	if err := validateCandidateEvidence(unit, &candidate); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("validateCandidateEvidence() error = %v", err)
	}
}

func TestBuildPromptDropsContextBeforeNewEvidence(t *testing.T) {
	batch := contractChatBatch()
	unit := batch.Units[0]
	unit.Messages[0].Content = "old-marker " + strings.Repeat("x", 5000)
	prompt, err := BuildPrompt(batch, unit, nil, time.Now(), PromptOptions{
		PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 4000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if strings.Contains(prompt.User, "old-marker") || !strings.Contains(prompt.User, "new request") {
		t.Fatalf("prompt = %q", prompt.User)
	}
}

func TestBuildPromptRejectsUnencodableMemory(t *testing.T) {
	batch := contractChatBatch()
	_, err := BuildPrompt(batch, batch.Units[0], []map[string]any{{"bad": func() {}}}, time.Now(), PromptOptions{
		PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 60000,
	})
	if err == nil || !strings.Contains(err.Error(), "encode extraction memory") {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
}

func TestBuildPromptFiltersM3Memory(t *testing.T) {
	batch := contractChatBatch()
	prompt, err := BuildPrompt(batch, batch.Units[0], []map[string]any{
		{"memory": "self-loop", "metadata": map[string]any{"source": "m3"}},
		{"memory": "background", "metadata": map[string]any{"source": "decision"}},
	}, time.Now(), PromptOptions{PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 60000})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if strings.Contains(prompt.User, "self-loop") || !strings.Contains(prompt.User, "background") {
		t.Fatalf("prompt = %q", prompt.User)
	}
}

func TestPrepareResultsRejectsIncompleteFingerprint(t *testing.T) {
	store := &PipelineStore{location: time.UTC}
	batch := contractChatBatch()
	candidate := contractStrictCandidate()
	candidate.Slots["change_summary"] = nil
	candidate.InfoSufficient = false
	candidate.MissingInfo = []string{"change_summary"}
	_, err := store.prepareResults(batch, []UnitExtraction{{UnitKey: "chat", Candidates: []ResolvedCandidate{resolvedCandidate(candidate)}}})
	if !errors.Is(err, ErrFingerprintIncomplete) {
		t.Fatalf("prepareResults() error = %v", err)
	}
}

func TestPrepareResultsDerivesLeaderAssigner(t *testing.T) {
	store := &PipelineStore{location: time.UTC}
	batch := contractChatBatch()
	prepared, err := store.prepareResults(batch, []UnitExtraction{{UnitKey: "chat", Candidates: []ResolvedCandidate{resolvedCandidate(contractStrictCandidate())}}})
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
	slots := make(map[string]any, len(allowedSlots))
	for name := range allowedSlots {
		slots[name] = nil
	}
	slots["repo_ref"] = "jarvis"
	slots["change_summary"] = "modify auth"
	return Candidate{
		ActionType: "code_change", Title: "Modify auth", Description: "Implement the requested auth change",
		CommitmentStrength: "firm", SourceMessageIDs: []string{"om_new"}, SourceQuote: "new request: modify auth",
		Slots: slots, InfoSufficient: true, MissingInfo: []string{},
	}
}
