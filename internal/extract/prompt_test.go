package extract

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const testM3SystemPrompt = "test M3 system prompt\n{{WORK_RULES}}"

func TestBuildPromptSeparatesEvidenceFromBackground(t *testing.T) {
	unit := ConversationUnit{
		Key: "chat",
		Messages: []MessageContext{
			{MessageID: "om_context", Content: "旧背景", CreateTime: 1_700_000_000_000, IsNew: false, Extractable: true},
			{MessageID: "om_new", Content: "请修改鉴权逻辑", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true},
		},
	}
	batch := ChatBatch{Group: GroupContext{ID: 1, ChatID: "oc_1", Name: "研发群"}}
	counts := []FactCount{{SubjectType: "project", SubjectID: 3, Label: "鉴权", Today: 1, Last7Days: 2}}
	prompt, err := BuildPrompt(batch, unit, counts, time.Unix(1_700_000_100, 0), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	for _, want := range []string{"[context] msg_id=om_context", "[new] msg_id=om_new", "世界事实（明细未展开）", "project:3「鉴权」今日 1 条、近 7 天 2 条"} {
		if !strings.Contains(prompt.User, want) {
			t.Fatalf("prompt user missing %q:\n%s", want, prompt.User)
		}
	}
	if strings.Contains(prompt.User, "鉴权改造由张三负责") {
		t.Fatalf("prompt still contains fact body:\n%s", prompt.User)
	}
}

func TestBuildPromptInjectsSkills(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "om_new", Content: "通知同事", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true,
	}}}
	prompt, err := BuildPrompt(ChatBatch{Group: GroupContext{ChatID: "oc_1"}}, unit, nil, time.Now(), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 20_000,
		Skills: "BEGIN_AVAILABLE_SKILLS\n- feishu-send-message\nEND_AVAILABLE_SKILLS",
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt.System, "feishu-send-message") {
		t.Fatalf("system prompt missing skill catalog:\n%s", prompt.System)
	}
}

func TestBuildPromptInjectsSharedMemoryAsTrustedSystemBlock(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "om_new", Content: "请跟进", IsNew: true, Extractable: true,
	}}}
	prompt, err := BuildPrompt(
		ChatBatch{Group: GroupContext{ChatID: "oc_1"}},
		unit,
		nil,
		time.Now(),
		PromptOptions{InitiativeLevel: "normal",
			SystemPrompt: testM3SystemPrompt, PrincipalOpenID: "ou_owner",
			Location: time.UTC, MaxChars: 20_000, SharedMemory: "固定验收标准：先核验原文",
		},
	)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	for _, want := range []string{"BEGIN_SHARED_MEMORY", "固定验收标准：先核验原文", "END_SHARED_MEMORY"} {
		if !strings.Contains(prompt.System, want) {
			t.Fatalf("system prompt missing shared memory %q:\n%s", want, prompt.System)
		}
	}
	if strings.Contains(prompt.User, "固定验收标准") {
		t.Fatalf("shared memory leaked into untrusted user prompt:\n%s", prompt.User)
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
		PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt, PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 5_000},
	)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if strings.Contains(prompt.User, "om_context") || !strings.Contains(prompt.User, "om_new") {
		t.Fatalf("context trimming result is incorrect:\n%s", prompt.User)
	}
}

func TestBuildPromptReportsOversizedNewEvidence(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "om_new", Content: strings.Repeat("新消息", 10_000), IsNew: true, Extractable: true,
	}}}
	_, err := BuildPrompt(
		ChatBatch{Group: GroupContext{ChatID: "oc_1"}}, unit, nil, time.Now(),
		PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt, PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 5_000},
	)
	if !errors.Is(err, ErrPromptTooLarge) {
		t.Fatalf("BuildPrompt() error = %v, want ErrPromptTooLarge", err)
	}
}

func TestBuildPromptKeepsOneCompleteNewMessageAtCoarseLimit(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{
		{MessageID: "om_context", Content: strings.Repeat("旧背景", 5_000), Extractable: true},
		{MessageID: "om_new", Content: strings.Repeat("新消息", 10_000), IsNew: true, Extractable: true},
	}}
	prompt, err := BuildPrompt(
		ChatBatch{Group: GroupContext{ChatID: "oc_1"}}, unit, nil, time.Now(),
		PromptOptions{InitiativeLevel: "normal",
			SystemPrompt: testM3SystemPrompt, PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 5_000,
			AllowSingleNewOverLimit: true,
		},
	)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if strings.Contains(prompt.User, "om_context") || !strings.Contains(prompt.User, "om_new") {
		t.Fatalf("single-message coarse limit result is incorrect")
	}
}

func TestRenderParticipantsKeepsIdentity(t *testing.T) {
	rendered := renderParticipants([]ParticipantContext{
		{OpenID: "ou_leader", Name: "老板", Role: "leader", IsLeader: true, Title: "负责人"},
		{OpenID: "ou_peer", Name: "同事", Role: "colleague"},
	})
	for _, want := range []string{`name="老板"`, `role=leader`, `title="负责人"`, `name="同事"`} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("renderParticipants missing %q:\n%s", want, rendered)
		}
	}
}

func TestBuildPromptCarriesMessageType(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID:   "om_1",
		MessageType: "post",
		Content:     "周会结论：下周三前完成灰度",
		Mentions:    json.RawMessage(`[{"id":"ou_owner","key":"@_user_1","name":"负责人"}]`),
		CreateTime:  1_700_000_001_000,
		IsNew:       true,
		Extractable: true,
	}}}
	prompt, err := BuildPrompt(
		ChatBatch{Group: GroupContext{ChatID: "oc_1"}},
		unit,
		nil,
		time.Unix(1_700_000_100, 0),
		PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt, PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 20_000},
	)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt.User, "message_type=post") {
		t.Fatalf("prompt missing message_type:\n%s", prompt.User)
	}
	if !strings.Contains(prompt.User, `mentions=[{"id":"ou_owner","key":"@_user_1","name":"负责人"}]`) {
		t.Fatalf("prompt missing mention identity:\n%s", prompt.User)
	}
}

func TestRenderPrincipalInjectsSelfAndLeader(t *testing.T) {
	rendered := renderPrincipal(&PrincipalContext{
		OpenID: "ou_me", Name: "我", Department: "平台", Title: "工程师",
		Summary:      "负责 Agent 基建",
		LeaderOpenID: "ou_boss", LeaderName: "测试主管",
	})
	for _, want := range []string{`name="我"`, `leader_open_id=ou_boss leader_name="测试主管"`, "负责 Agent 基建"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("renderPrincipal missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderPrincipalNilShowsHint(t *testing.T) {
	if got := renderPrincipal(nil); !strings.Contains(got, "未设置") {
		t.Fatalf("renderPrincipal(nil) = %q, want a not-set hint", got)
	}
}

func TestBuildPromptCarriesPrincipalAndProjects(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{
		{MessageID: "om_new", Content: "看下这个数据", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true},
	}}
	batch := ChatBatch{
		Group:     GroupContext{ID: 1, ChatID: "oc_1", Name: "研发群"},
		Principal: &PrincipalContext{OpenID: "ou_me", Name: "我", LeaderName: "测试主管", LeaderOpenID: "ou_boss"},
		OtherProjects: []OtherProjectContext{
			{ID: 9, Code: "runtime", Name: "Agent Runtime", Role: "participant"},
		},
	}
	prompt, err := BuildPrompt(batch, unit, nil, time.Unix(1_700_000_100, 0), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	for _, want := range []string{"# 我的背景(principal)", "leader_name=\"测试主管\"", "# 我的其他项目（精简", "name=\"Agent Runtime\""} {
		if !strings.Contains(prompt.User, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt.User)
		}
	}
}

func TestBuildPromptCarriesGroupAnnouncement(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "om_new", Content: "看下这个", CreateTime: 1_700_000_001_000,
		IsNew: true, Extractable: true,
	}}}
	prompt, err := BuildPrompt(ChatBatch{Group: GroupContext{
		ID: 1, ChatID: "oc_1", Name: "Agent Runtime",
		Description: "本群负责 runtime 项目，代码仓库为 llm_agent_core。",
	}}, unit, nil, time.Unix(1_700_000_100, 0), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt.User, "群公告：本群负责 runtime 项目，代码仓库为 llm_agent_core。") {
		t.Fatalf("prompt missing group announcement:\n%s", prompt.User)
	}
}

func TestBuildPromptCarriesTrustedWorkRules(t *testing.T) {
	prompt, err := BuildPrompt(ChatBatch{Group: GroupContext{ChatID: "oc_1"}}, ConversationUnit{
		Key: "chat", Messages: []MessageContext{{MessageID: "m1", Content: "做一下", IsNew: true, Extractable: true}},
	}, nil, time.Now(), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 20_000,
		WorkRules: "BEGIN_WORK_RULES\n- 先遵守规则\nEND_WORK_RULES",
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt.System, "BEGIN_WORK_RULES") || strings.Contains(prompt.User, "BEGIN_WORK_RULES") {
		t.Fatalf("work rules must be in trusted system prompt only: %+v", prompt)
	}
}

func TestBuildPromptRendersSummaryAndFactCountsOnly(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "om_new", Content: "看下这个", CreateTime: 1_700_000_001_000,
		IsNew: true, Extractable: true,
	}}}
	batch := ChatBatch{
		Principal: &PrincipalContext{
			OpenID: "ou_me", Name: "我", Summary: "我负责公会 Agent 基建。",
		},
		Project: &ProjectContext{
			ID: 44, Code: "agency", Name: "公会 Agent 基建", Role: "owner",
			Status: "active", Priority: 1, Summary: "公会侧个人 agent 系统，正在收口世界模型。",
		},
		Group: GroupContext{
			ID: 7, ChatID: "oc_1", Name: "公会群",
			Description: "飞书群公告原文",
			Summary:     "这个群跟进公会 Agent 基建。",
		},
		OtherProjects: []OtherProjectContext{
			{ID: 9, Code: "runtime", Name: "Agent Runtime", Role: "participant", Status: "active", Priority: 2},
		},
	}
	counts := []FactCount{{
		SubjectType: "project", SubjectID: 44, Label: "公会 Agent 基建", Today: 23, Last7Days: 187,
	}}
	prompt, err := BuildPrompt(batch, unit, counts, time.Unix(1_700_000_100, 0), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	for _, want := range []string{
		"我负责公会 Agent 基建。",
		"公会侧个人 agent 系统，正在收口世界模型。",
		"这个群跟进公会 Agent 基建。",
		"群公告：飞书群公告原文",
		"project:44「公会 Agent 基建」今日 23 条、近 7 天 187 条 —— 需要细节先用 list-facts 查证据索引，再沿 source 指针读原始材料。",
	} {
		if !strings.Contains(prompt.User, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt.User)
		}
	}
	for _, forbidden := range []string{
		"description=", "repos=", "tech_stack=", "key_decisions=", "timeline=", "notes=",
		"负责方向/背景", "喜好/工作偏好", "人工背景：", "relation=", "comm_style=",
		"desc=",
	} {
		if strings.Contains(prompt.User, forbidden) {
			t.Fatalf("prompt still contains deleted field %q:\n%s", forbidden, prompt.User)
		}
	}
}

func TestRenderFactCountsOmitsFactBodies(t *testing.T) {
	rendered := renderFactCounts([]FactCount{{
		SubjectType: "project", SubjectID: 44, Label: "公会 Agent 基建", Today: 23, Last7Days: 187,
	}})
	if !strings.Contains(rendered, "project:44「公会 Agent 基建」今日 23 条、近 7 天 187 条") {
		t.Fatalf("renderFactCounts missing count line:\n%s", rendered)
	}
	if !strings.Contains(rendered, "list-facts") {
		t.Fatalf("renderFactCounts missing drill-down hint:\n%s", rendered)
	}
	for _, forbidden := range []string{"鉴权改造由张三负责", "今天明细", "昨天压缩"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("renderFactCounts leaked fact body %q:\n%s", forbidden, rendered)
		}
	}
}
