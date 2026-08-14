package extract

import (
	"os"
	"strings"
	"testing"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/toolcatalog"
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
	facts := []contextsnap.Fact{
		{ID: 7, SubjectType: "project", SubjectID: 3, Description: "鉴权改造由张三负责", OccurredAt: "2026-07-30T10:00:00Z"},
	}
	prompt, err := BuildPrompt(batch, unit, facts, time.Unix(1_700_000_100, 0), PromptOptions{SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	for _, want := range []string{"[context] msg_id=om_context", "[new] msg_id=om_new", "鉴权改造由张三负责", "已沉淀的事实"} {
		if !strings.Contains(prompt.User, want) {
			t.Fatalf("prompt user missing %q:\n%s", want, prompt.User)
		}
	}
}

func TestToolCatalogIsSeparateFromSystemPrompt(t *testing.T) {
	t.Parallel()
	catalog, err := toolcatalog.Block(toolcatalog.StageExtract)
	if err != nil {
		t.Fatalf("toolcatalog.Block() error = %v", err)
	}
	if !strings.Contains(catalog, "jarvis-tools") {
		t.Fatalf("tool catalog missing jarvis-tools: %s", catalog)
	}
	raw, err := os.ReadFile("../../conf/prompts/m3-system-prompt.md")
	if err != nil {
		t.Fatalf("read M3 system prompt: %v", err)
	}
	systemPrompt := string(raw)
	if strings.Contains(systemPrompt, "jarvis-tools") || strings.Contains(systemPrompt, "lark-cli") {
		t.Fatalf("M3 system prompt must not contain tool instructions: %s", systemPrompt)
	}
}

func TestBuildPromptInjectsSkills(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "om_new", Content: "通知同事", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true,
	}}}
	prompt, err := BuildPrompt(ChatBatch{Group: GroupContext{ChatID: "oc_1"}}, unit, nil, time.Now(), PromptOptions{SystemPrompt: testM3SystemPrompt,
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
		PromptOptions{SystemPrompt: testM3SystemPrompt, PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 5_000},
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

func TestBuildPromptCarriesMessageType(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID:   "om_1",
		MessageType: "post",
		Content:     "周会结论：下周三前完成灰度",
		CreateTime:  1_700_000_001_000,
		IsNew:       true,
		Extractable: true,
	}}}
	prompt, err := BuildPrompt(
		ChatBatch{Group: GroupContext{ChatID: "oc_1"}},
		unit,
		nil,
		time.Unix(1_700_000_100, 0),
		PromptOptions{SystemPrompt: testM3SystemPrompt, PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 20_000},
	)
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt.User, "message_type=post") {
		t.Fatalf("prompt missing message_type:\n%s", prompt.User)
	}
}

func TestExtractionPromptDefinesTaskAdmissionBoundary(t *testing.T) {
	raw, err := os.ReadFile("../../conf/prompts/m3-system-prompt.md")
	if err != nil {
		t.Fatalf("read M3 system prompt: %v", err)
	}
	system := string(raw)
	// principal 每天都在调这份提示词的措辞，所以这里只锚定两类不该漂的东西：
	// 模型必须填的机器契约字段，以及两条曾经真的回归过的语义边界。散文表述
	// 不做断言——之前逐句断言的版本被一次正常的措辞调整弄红过。
	for _, want := range []string{
		// 机器契约：status 枚举与 schema 要求的八个字段必须出现在提示词里。
		"status=extracted",
		"status=observing",
		"action_type",
		"project_hint",
		"source_message_ids",
		"source_quote",
		"payload",
		// 语义边界一：M3 只做准入，不越界到执行阶段。
		"不制定执行方案",
		// 语义边界二：principal 直接给 Jarvis 的指令必须绕过价值判断。少了这条，
		// 强模型会把「让 jarvis 说句话」判成测试信息并丢弃。
		"principal 直接要求 Jarvis",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"主动发散补全",
		"候选路径与取舍",
		"在交给我之前尽量把背景查全",
	} {
		if strings.Contains(system, forbidden) {
			t.Fatalf("system prompt still contains execution-stage instruction %q", forbidden)
		}
	}
}

func TestRenderPrincipalInjectsSelfAndLeader(t *testing.T) {
	rendered := renderPrincipal(&PrincipalContext{
		OpenID: "ou_me", Name: "我", Department: "平台", Title: "工程师",
		Background: "负责 Agent 基建", Preferences: "偏好直接给结论",
		LeaderOpenID: "ou_boss", LeaderName: "测试主管",
	})
	for _, want := range []string{`name="我"`, `leader_open_id=ou_boss leader_name="测试主管"`, "负责 Agent 基建", "偏好直接给结论"} {
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
			{ID: 9, Code: "runtime", Name: "Agent Runtime", Role: "participant", Description: "codex 方案"},
		},
	}
	prompt, err := BuildPrompt(batch, unit, nil, time.Unix(1_700_000_100, 0), PromptOptions{SystemPrompt: testM3SystemPrompt,
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
		Description:    "本群负责 runtime 项目，代码仓库为 llm_agent_core。",
		BackgroundNote: "优先检查 llm_agent_core，涉及旧实现再查 openclaw。",
	}}, unit, nil, time.Unix(1_700_000_100, 0), PromptOptions{SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if !strings.Contains(prompt.User, "群公告：本群负责 runtime 项目，代码仓库为 llm_agent_core。") {
		t.Fatalf("prompt missing group announcement:\n%s", prompt.User)
	}
	if !strings.Contains(prompt.User, "人工背景：优先检查 llm_agent_core，涉及旧实现再查 openclaw。") {
		t.Fatalf("prompt missing group background note:\n%s", prompt.User)
	}
}

func TestBuildPromptCarriesTrustedWorkRules(t *testing.T) {
	prompt, err := BuildPrompt(ChatBatch{Group: GroupContext{ChatID: "oc_1"}}, ConversationUnit{
		Key: "chat", Messages: []MessageContext{{MessageID: "m1", Content: "做一下", IsNew: true, Extractable: true}},
	}, nil, time.Now(), PromptOptions{SystemPrompt: testM3SystemPrompt,
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
