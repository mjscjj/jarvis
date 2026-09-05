package extract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"jarvis/internal/contextpack"
)

func TestBuildContextSnapshotFreezesSummary(t *testing.T) {
	store := &PipelineStore{}
	summary := "公会侧个人 agent 系统。"
	groupSummary := "这个群跟进公会基建。"
	batch := ChatBatch{
		Principal: &PrincipalContext{OpenID: "ou_me", Name: "我", Summary: "我负责公会基建。"},
		Project: &ProjectContext{
			ID: 44, Name: "公会 Agent 基建", Role: "owner", Status: "active", Priority: 1, Summary: summary,
		},
		Group: GroupContext{ID: 7, ChatID: "oc_1", Name: "公会群", Summary: groupSummary},
	}
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "om_1", Content: "请跟进", CreateTime: 1, IsNew: true, Extractable: true,
	}}}
	projectID := uint64(44)
	snapshot, err := store.buildContextSnapshot(t.Context(), batch, unit, Candidate{SourceMessageIDs: []string{"om_1"}}, &projectID, nil)
	if err != nil {
		t.Fatalf("buildContextSnapshot: %v", err)
	}
	if snapshot.Principal == nil || snapshot.Principal.Summary == nil || *snapshot.Principal.Summary != "我负责公会基建。" {
		t.Fatalf("principal.summary = %#v", snapshot.Principal)
	}
	if snapshot.Project == nil || snapshot.Project.Summary == nil || *snapshot.Project.Summary != summary {
		t.Fatalf("project.summary = %#v", snapshot.Project)
	}
	if snapshot.Group == nil || snapshot.Group.Summary == nil || *snapshot.Group.Summary != groupSummary {
		t.Fatalf("group.summary = %#v", snapshot.Group)
	}
}

func TestFrozenMaterialsKeepAdvertisedProjectCatalog(t *testing.T) {
	projectID := uint64(44)
	batch := ChatBatch{
		Group:         GroupContext{ID: 7, ChatID: "oc_1"},
		Project:       &ProjectContext{ID: projectID, Name: "唯一项目"},
		OtherProjects: []OtherProjectContext{{ID: projectID, Name: "唯一项目"}},
	}
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{MessageID: "om_1", Content: "请处理"}}}
	candidate := Candidate{SourceMessageIDs: []string{"om_1"}, Annotation: json.RawMessage(`{"background":"相关项目"}`)}
	snapshot, err := (&PipelineStore{}).buildContextSnapshot(t.Context(), batch, unit, candidate, &projectID, nil)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := snapshot.Encode()
	if err != nil {
		t.Fatal(err)
	}
	packet, err := contextpack.Freeze([]byte(`{}`), capture, "brief", candidate.Annotation)
	if err != nil {
		t.Fatal(err)
	}
	material, err := contextpack.Read(packet, "other_projects", "")
	if err != nil || !strings.Contains(string(material), "唯一项目") {
		t.Fatalf("advertised material lost: %s %v", material, err)
	}
}

func TestSnapshotConversationKeepsEntireAdmittedUnit(t *testing.T) {
	messages := make([]MessageContext, 30)
	for i := range messages {
		messages[i] = MessageContext{MessageID: fmt.Sprintf("om_%02d", i+1)}
	}

	got := snapshotConversation(ConversationUnit{Messages: messages})
	if len(got) != 30 {
		t.Fatalf("snapshot conversation length = %d, want 30", len(got))
	}
	if got[0].MessageID != "om_01" || got[len(got)-1].MessageID != "om_30" {
		t.Fatalf("snapshot conversation range = %s..%s, want om_01..om_30", got[0].MessageID, got[len(got)-1].MessageID)
	}
}
