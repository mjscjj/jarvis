package extract

import (
	"strings"
	"testing"
	"time"
)

func TestBuildPromptSeparatesEvidenceFromBackground(t *testing.T) {
	unit := ConversationUnit{
		Key: "chat",
		Messages: []MessageContext{
			{MessageID: "om_context", Content: "旧背景", CreateTime: 1_700_000_000_000, IsNew: false, Extractable: true},
			{MessageID: "om_new", Content: "请修改鉴权逻辑", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true},
		},
	}
	batch := ChatBatch{Group: GroupContext{ID: 1, ChatID: "oc_1", Name: "研发群"}}
	memories := []map[string]any{
		{"memory": "must be filtered", "metadata": map[string]any{"source": "m3"}},
		{"memory": "stable project fact", "metadata": map[string]any{"source": "m2"}},
	}
	prompt, err := BuildPrompt(batch, unit, memories, time.Unix(1_700_000_100, 0), PromptOptions{
		PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	for _, want := range []string{"[context] msg_id=om_context", "[new] msg_id=om_new", "stable project fact"} {
		if !strings.Contains(prompt.User, want) {
			t.Fatalf("prompt user missing %q:\n%s", want, prompt.User)
		}
	}
	if strings.Contains(prompt.User, "must be filtered") {
		t.Fatalf("prompt includes M3 feedback memory:\n%s", prompt.User)
	}
}

func TestBuildPromptTrimsContextBeforeFailing(t *testing.T) {
	unit := ConversationUnit{
		Key: "chat",
		Messages: []MessageContext{
			{MessageID: "om_context", Content: strings.Repeat("背景", 10_000), CreateTime: 1_700_000_000_000, IsNew: false, Extractable: true},
			{MessageID: "om_new", Content: "请跟进发布", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true},
		},
	}
	prompt, err := BuildPrompt(
		ChatBatch{Group: GroupContext{ID: 1, ChatID: "oc_1"}}, unit, nil, time.Now(),
		PromptOptions{PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 5_000},
	)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if strings.Contains(prompt.User, "om_context") || !strings.Contains(prompt.User, "om_new") {
		t.Fatalf("context trimming result is incorrect:\n%s", prompt.User)
	}
}

func TestRenderParticipantsInjectsCommStyle(t *testing.T) {
	rendered := renderParticipants([]ParticipantContext{
		{OpenID: "ou_leader", Name: "老板", Role: "leader", IsLeader: true, Relation: "直属领导", CommStyle: "指令常以「看下」隐含表达"},
		{OpenID: "ou_peer", Name: "同事", Role: "colleague"},
	})
	for _, want := range []string{`relation="直属领导"`, `comm_style="指令常以「看下」隐含表达"`} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("renderParticipants missing %q:\n%s", want, rendered)
		}
	}
	// A participant without comm_style must not emit an empty comm_style token.
	if strings.Contains(rendered, `name="同事" role=colleague is_leader=false comm_style`) {
		t.Fatalf("renderParticipants emitted empty comm_style for peer:\n%s", rendered)
	}
}

func TestSalientQueryRequiresExtractableNewMessage(t *testing.T) {
	_, err := SalientQuery(ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "om_context", Content: "only context", IsNew: false, Extractable: true,
	}}})
	if err == nil {
		t.Fatal("SalientQuery() accepted a unit without extractable new messages")
	}
}
