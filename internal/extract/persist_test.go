package extract

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"encoding/json"
	"jarvis/internal/contextpack"
	"jarvis/internal/contextsnap"
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
			Participants: []ParticipantContext{{
				OpenID: "ou_leader", Name: "Leader", Role: "leader", Title: "负责人",
				IsLeader: true, Summary: "直属领导，常用简短交办",
			}},
			Resources: []ResourceContext{{
				ID: 7, ResourceType: "doc", DocToken: "doc_1", Name: "设计文档",
				ExtractedText: "鉴权改造方案",
			}},
		}},
		OpenTodos: []OpenTodoContext{{ID: 8, ActionType: "code_change", Title: "旧鉴权任务", Status: "need_info"}},
		OtherProjects: []OtherProjectContext{{
			ID: 9, Code: "runtime", Name: "Agent Runtime", Role: "participant",
			Status: "active", Priority: 2,
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
	material, err := contextpack.Read(prepared[0].Content, "full", "")
	var packet struct {
		Capture json.RawMessage `json:"capture"`
	}
	if err := json.Unmarshal(material, &packet); err != nil {
		t.Fatal(err)
	}
	snapshot, err := contextsnap.Decode(packet.Capture)
	if err != nil {
		t.Fatalf("decode prepared context snapshot: %v", err)
	}
	if snapshot.Assigner == nil || snapshot.Assigner.OpenID != "ou_leader" || snapshot.Assigner.Title == nil || *snapshot.Assigner.Title != "负责人" {
		t.Fatalf("snapshot assigner = %#v", snapshot.Assigner)
	}
	if snapshot.Assigner.Summary == nil || *snapshot.Assigner.Summary != "直属领导，常用简短交办" {
		t.Fatalf("snapshot assigner.summary = %#v", snapshot.Assigner.Summary)
	}
	if len(snapshot.Participants) != 1 || len(snapshot.Resources) != 1 || len(snapshot.OtherProjects) != 1 {
		t.Fatalf("snapshot did not freeze full M3 context: %#v", snapshot)
	}
	// batch.OpenTodos feeds the M3 dedup prompt but must not be frozen: M5 reads
	// live Todos and Tasks at execution time instead.
	if strings.Contains(string(prepared[0].Content), "旧鉴权任务") {
		t.Fatalf("snapshot froze open todos:\n%s", prepared[0].Content)
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
	claimed := nilMessage("user", "请处理 123", true)
	claimed.ExtractionSkipped = true
	if extractableMessage(claimed) {
		t.Fatal("extractableMessage() accepted a route-claimed message")
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
		ActionType: "code_change", Status: "extracted", Title: "修改鉴权", Target: "jarvis 鉴权逻辑重构",
		Payload: "按讨论修改鉴权逻辑并合入；归属 jarvis 项目，仓库 jarvis。", SourceMessageIDs: []string{"om_1"},
		SourceQuote: "请修改鉴权逻辑",
	}
}

func resolvedCandidate(candidate Candidate) ResolvedCandidate {
	return ResolvedCandidate{Candidate: candidate, Semantic: SemanticResolution{Vector: []float32{1}}}
}

// TestM3OwnedTodoStatuses pins which states re-extraction may still move a clue
// between. M3 owns the two it can emit; anything a downstream stage set must
// survive re-extraction, or an already-routed clue would be pulled back into
// materialization and mint a duplicate Task.
func TestM3OwnedTodoStatuses(t *testing.T) {
	for _, status := range []string{"extracted", "observing"} {
		if !m3OwnedTodoStatuses[status] {
			t.Fatalf("m3OwnedTodoStatuses[%q] = false, want true", status)
		}
	}
	for _, status := range []string{"materialized"} {
		if m3OwnedTodoStatuses[status] {
			t.Fatalf("m3OwnedTodoStatuses[%q] = true, want false", status)
		}
	}
	for status := range m3OwnedTodoStatuses {
		if _, ok := allowedTodoStatuses[status]; !ok {
			t.Fatalf("m3-owned status %q is not an allowed Todo status", status)
		}
	}
}
