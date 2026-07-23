package extract

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

func TestCodexToolGuidanceIncludesScheduledTasks(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"list-scheduled-tasks", "create-scheduled-task", "delete-scheduled-task", "context_snapshot", `schedule_type:"once"`, "run_at"} {
		if !strings.Contains(CodexToolGuidance, want) {
			t.Fatalf("CodexToolGuidance missing %q", want)
		}
	}
}

// 共享记忆非空时，M3 应把 BEGIN_SHARED_MEMORY block 追加到 system 段（受信任指令区）；
// 为空时不注入。
func TestBuildPromptInjectsSharedMemory(t *testing.T) {
	unit := ConversationUnit{
		Key: "chat",
		Messages: []MessageContext{
			{MessageID: "om_new", Content: "请修改鉴权逻辑", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true},
		},
	}
	batch := ChatBatch{Group: GroupContext{ID: 1, ChatID: "oc_1", Name: "研发群"}}

	empty, err := BuildPrompt(batch, unit, nil, time.Unix(1_700_000_100, 0), PromptOptions{
		PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	if strings.Contains(empty.System, "BEGIN_SHARED_MEMORY") {
		t.Fatalf("empty shared memory must not inject block:\n%s", empty.System)
	}

	prompt, err := BuildPrompt(batch, unit, nil, time.Unix(1_700_000_100, 0), PromptOptions{
		PrincipalOpenID: "ou_owner", Location: time.UTC, MaxChars: 20_000,
		SharedMemory: "采集死锁的坑：别在事务里调 lark-cli",
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	for _, want := range []string{"BEGIN_SHARED_MEMORY", "采集死锁的坑：别在事务里调 lark-cli", "可信"} {
		if !strings.Contains(prompt.System, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt.System)
		}
	}
}

func TestBuildPromptInjectsSkills(t *testing.T) {
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{
		MessageID: "om_new", Content: "通知同事", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true,
	}}}
	prompt, err := BuildPrompt(ChatBatch{Group: GroupContext{ChatID: "oc_1"}}, unit, nil, time.Now(), PromptOptions{
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

func TestSalientQueryCapsToLastMessages(t *testing.T) {
	messages := make([]MessageContext, 0, 30)
	for i := 0; i < 30; i++ {
		messages = append(messages, MessageContext{
			MessageID:   "om",
			Content:     fmt.Sprintf("消息%d", i),
			IsNew:       true,
			Extractable: true,
		})
	}
	query, err := SalientQuery(ConversationUnit{Key: "chat", Messages: messages})
	if err != nil {
		t.Fatalf("SalientQuery() error = %v", err)
	}
	lines := strings.Split(query, "\n")
	if len(lines) != salientQueryMaxMessages {
		t.Fatalf("SalientQuery() returned %d lines, want %d", len(lines), salientQueryMaxMessages)
	}
	// Must keep the latest messages (intent lives at the end), dropping oldest.
	if strings.Contains(query, "消息0\n") || strings.Contains(query, "消息9\n") {
		t.Fatalf("SalientQuery() kept stale early messages:\n%s", query)
	}
	if !strings.Contains(query, "消息29") {
		t.Fatalf("SalientQuery() dropped the newest message:\n%s", query)
	}
}

func TestSalientQueryCapsLongMeetingEvidenceForMemorySearch(t *testing.T) {
	query, err := SalientQuery(ConversationUnit{Key: "meeting", Messages: []MessageContext{{
		MessageID:   "meeting:1",
		Content:     "开头行动项\n" + strings.Repeat("会议逐字稿", 2000) + "\n结尾行动项",
		IsNew:       true,
		Extractable: true,
	}}})
	if err != nil {
		t.Fatalf("SalientQuery() error = %v", err)
	}
	if got := utf8.RuneCountInString(query); got != salientQueryMaxChars {
		t.Fatalf("SalientQuery() runes = %d, want %d", got, salientQueryMaxChars)
	}
	for _, want := range []string{"开头行动项", "记忆检索查询已截断", "结尾行动项"} {
		if !strings.Contains(query, want) {
			t.Fatalf("SalientQuery() missing %q", want)
		}
	}
}

func TestExtractionPromptLeavesMeetingCaptureResultDecisionToAgent(t *testing.T) {
	system := fmt.Sprintf(systemPromptTemplate, "ou_owner")
	for _, want := range []string{
		"[会议妙记采集结果]",
		"自行判断",
		"允许返回 candidates=[]",
		"禁止把采集模块的状态机械映射成固定 Todo",
		"source_message_ids 必须引用触发判断的那条 [new]",
		"不能改引旧会议消息或其他 context",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
	for _, forbidden := range []string{"必须为 principal 提取一条 manual_followup", "决定是否申请对应妙记的查看权限"} {
		if strings.Contains(system, forbidden) {
			t.Fatalf("system prompt contains hard-coded decision %q", forbidden)
		}
	}
}

func TestRenderPrincipalInjectsSelfAndLeader(t *testing.T) {
	rendered := renderPrincipal(&PrincipalContext{
		OpenID: "ou_me", Name: "我", Department: "平台", Title: "工程师",
		Background: "负责 Agent 基建", Preferences: "偏好直接给结论",
		LeaderOpenID: "ou_boss", LeaderName: "严亮",
	})
	for _, want := range []string{`name="我"`, `leader_open_id=ou_boss leader_name="严亮"`, "负责 Agent 基建", "偏好直接给结论"} {
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
		Principal: &PrincipalContext{OpenID: "ou_me", Name: "我", LeaderName: "严亮", LeaderOpenID: "ou_boss"},
		OtherProjects: []OtherProjectContext{
			{ID: 9, Code: "runtime", Name: "Agent Runtime", Role: "participant", Description: "codex 方案"},
		},
	}
	prompt, err := BuildPrompt(batch, unit, nil, time.Unix(1_700_000_100, 0), PromptOptions{
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 20_000,
	})
	if err != nil {
		t.Fatalf("BuildPrompt() error = %v", err)
	}
	for _, want := range []string{"# 我的背景(principal)", "leader_name=\"严亮\"", "# 我的其他项目（精简", "name=\"Agent Runtime\""} {
		if !strings.Contains(prompt.User, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt.User)
		}
	}
}

func TestBuildPromptCarriesTrustedWorkRules(t *testing.T) {
	prompt, err := BuildPrompt(ChatBatch{Group: GroupContext{ChatID: "oc_1"}}, ConversationUnit{
		Key: "chat", Messages: []MessageContext{{MessageID: "m1", Content: "做一下", IsNew: true, Extractable: true}},
	}, nil, time.Now(), PromptOptions{
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
