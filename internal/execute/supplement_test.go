package execute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"

	"jarvis/internal/datatypes"
)

const (
	testToolCatalog    = "BEGIN_AVAILABLE_TOOLS\n- fixture-tool\nEND_AVAILABLE_TOOLS"
	testM5SystemPrompt = "test M5 system prompt\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}"
)

func TestAppendExecutionSupplement(t *testing.T) {
	at1 := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	raw, err := appendExecutionSupplement(nil, "优先用季度模板", "backend", at1)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	at2 := time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)
	second, err := appendExecutionSupplement(raw, "只发给 A", "backend", at2)
	if err != nil {
		t.Fatalf("append second: %v", err)
	}
	items, err := decodeExecutionSupplements(second)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 2 || items[0].Note != "优先用季度模板" || items[1].Note != "只发给 A" {
		t.Fatalf("items = %#v", items)
	}
}

func TestBuildExecutionPromptIncludesExecutionSupplements(t *testing.T) {
	supplements, err := encodeExecutionSupplements([]ExecutionSupplement{{Note: "标题要包含季度", At: "2026-07-20T12:00:00Z"}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	task := &domain.Task{
		ID: 9, Title: "发提醒", ActionType: "summary_post",
		SourcePayload: datatypes.JSON(`{"steps":["send"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
		ExecutionSupplements: datatypes.JSON(supplements),
	}
	prompt, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !strings.Contains(prompt, "执行阶段补充") || !strings.Contains(prompt, "标题要包含季度") {
		t.Fatalf("prompt missing supplements: %s", prompt)
	}
	for _, obsolete := range []string{`"decision_context"`, `"decision_direction"`} {
		if strings.Contains(prompt, obsolete) {
			t.Fatalf("prompt contains removed decision field %q: %s", obsolete, prompt)
		}
	}
}

// 共享记忆非空时，execution prompt 应在 TASK_CONTEXT 之前包含 BEGIN_SHARED_MEMORY 标记
// 与内容；为空时不包含。
func TestBuildExecutionPromptInjectsSharedMemory(t *testing.T) {
	task := &domain.Task{
		ID: 9, Title: "发提醒", ActionType: "summary_post",
		SourcePayload: datatypes.JSON(`{"steps":["send"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	empty, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if strings.Contains(empty, "BEGIN_SHARED_MEMORY") {
		t.Fatalf("empty shared memory must not inject block:\n%s", empty)
	}
	prompt, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "lark-cli 的 token 存在 ~/.lark 里", "", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{"BEGIN_SHARED_MEMORY", "lark-cli 的 token 存在 ~/.lark 里", "可信"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Index(prompt, "BEGIN_SHARED_MEMORY") >= strings.Index(prompt, "BEGIN_TASK_CONTEXT") {
		t.Fatalf("shared memory block must precede TASK_CONTEXT:\n%s", prompt)
	}
}

func TestBuildExecutionPromptInjectsWorkRules(t *testing.T) {
	task := &domain.Task{
		ID: 9, Title: "发提醒", ActionType: "summary_post",
		SourcePayload: datatypes.JSON(`{"steps":["send"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	prompt, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "BEGIN_WORK_RULES\n- 禁止直接私聊\nEND_WORK_RULES", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{"BEGIN_WORK_RULES", "禁止直接私聊"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Index(prompt, "BEGIN_WORK_RULES") >= strings.Index(prompt, "BEGIN_TASK_CONTEXT") {
		t.Fatalf("work rule block must precede TASK_CONTEXT:\n%s", prompt)
	}
}

func TestBuildExecutionPromptInjectsSkills(t *testing.T) {
	task := &domain.Task{
		ID: 9, Title: "发提醒", ActionType: "summary_post",
		SourcePayload: datatypes.JSON(`{"steps":["send"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	prompt, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "BEGIN_AVAILABLE_SKILLS\n- feishu-send-message\nEND_AVAILABLE_SKILLS", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !strings.Contains(prompt, "feishu-send-message") || strings.Index(prompt, "BEGIN_AVAILABLE_SKILLS") >= strings.Index(prompt, "BEGIN_TASK_CONTEXT") {
		t.Fatalf("skill catalog must precede TASK_CONTEXT:\n%s", prompt)
	}
	for _, want := range []string{"BEGIN_AVAILABLE_TOOLS", "fixture-tool"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing tool catalog %q:\n%s", want, prompt)
		}
	}
}

func TestBuildExecutionPromptIncludesPreviousRuns(t *testing.T) {
	task := &domain.Task{
		ID: 10, Title: "告诉唐建科 PSM", ActionType: "investigate",
		SourcePayload: datatypes.JSON(`{"steps":["reply"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	finished := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	summary := "已建群并解释 PSM 是 Product-Service-Module"
	prior := []priorRunSummary{{
		RunID: 3, Status: "succeeded", Summary: summary,
		StartedAt: "2026-07-21T07:55:00Z", FinishedAt: finished.Format(time.RFC3339),
	}}
	prompt, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", prior)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"previous_runs"`, `"run_id":3`, summary, ExecutionPromptVersion,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildExecutionPromptKeepsOnlyUsefulTaskHints(t *testing.T) {
	task := &domain.Task{
		ID: 12, Title: "评测截图", ActionType: "notify_principal", Target: "评测截图影响面",
		Background: datatypes.JSON(`{"snapshot_version":"v1"}`), SourcePayload: datatypes.JSON(`{"request":"评测截图"}`),
	}
	prompt, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"title_hint":"评测截图"`,
		`"target_hint":"评测截图影响面"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing hint field %q:\n%s", want, prompt)
		}
	}
	for _, obsolete := range []string{`"action_type_hint":`, `"action_type":`, `"plan":`, `"decision_payload":`, `"decision_direction":`, `"decision_context":`} {
		if strings.Contains(prompt, obsolete) {
			t.Fatalf("execution prompt still exposes upstream semantics as authoritative field %q:\n%s", obsolete, prompt)
		}
	}
}

func TestBuildExecutionPromptProjectsFrozenBackground(t *testing.T) {
	projectCode := "jarvis"
	groupName := "公会 AI 突击群"
	assignerName := "测试委托人"
	assignerRole := "leader"
	assignerRelation := "manager"
	snapshot, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		CapturedAt:      "2026-08-06T03:00:00Z",
		Principal:       &contextsnap.Principal{OpenID: "ou_principal", Name: "principal", Background: stringPtr("完整个人背景不应进执行简报")},
		Project: &contextsnap.Project{
			ID: 7, Code: &projectCode, Name: "Jarvis", Role: "owner", Status: "active",
			KeyDecisions: json.RawMessage(`[{"decision":"完整项目决策不应进执行简报"}]`),
		},
		Group:    &contextsnap.Group{ID: 9, ChatID: "oc_group", Name: &groupName, Description: stringPtr("完整群背景不应进执行简报")},
		Assigner: &contextsnap.Assigner{OpenID: "ou_assigner", Name: &assignerName, Role: &assignerRole, Relation: &assignerRelation},
		Messages: []contextsnap.Message{
			{MessageID: "om_1", ChatID: "oc_group", SenderOpenID: "ou_sender_1", SenderName: "发送人一", Content: "完整源消息正文必须进入执行简报", CreateTime: 1785985200},
			{MessageID: "om_1", Content: "重复引用也不应重复输出"},
			{MessageID: "om_2", ChatID: "oc_group", SenderOpenID: "ou_sender_2", SenderName: "发送人二", Content: "另一条完整正文也必须进入", CreateTime: 1785985260},
		},
		Conversation: []contextsnap.Message{{MessageID: "om_context", Content: "完整 conversation 不应进执行简报"}},
		Facts:        []contextsnap.Fact{{ID: 1, Description: "完整 fact 不应进执行简报"}},
		OpenTodos:    []contextsnap.OpenTodo{{ID: 2, Title: "其它 Todo 不应进执行简报"}},
		RecentTasks:  []contextsnap.RecentTask{{ID: 3, Summary: "其它 Task 摘要不应进执行简报"}},
	}).Encode()
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	lastProgressAt := time.Date(2026, 8, 6, 4, 5, 0, 0, time.UTC)
	task := &domain.Task{
		ID: 97, Title: "压缩上下文", ActionType: "code_change", Target: "M5 初始上下文",
		Status:         "waiting",
		Summary:        stringPtr("已经定位背景缺失，等待补齐 M5 首轮上下文"),
		LastProgressAt: &lastProgressAt,
		ProjectID:      uint64Ptr(7),
		SourcePayload:  datatypes.JSON(`{"source_quote":"请压缩上下文","source_message_ids":["om_1","om_2"]}`),
		Background:     datatypes.JSON(snapshot),
	}
	prompt, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "/workspace/jarvis", testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"execution_context"`, `"current_status":"waiting"`, `"current_summary":"已经定位背景缺失，等待补齐 M5 首轮上下文"`,
		`"last_progress_at":"2026-08-06T04:05:00Z"`, `"principal":{"open_id":"ou_principal","name":"principal"}`,
		`"id":7`, `"code":"jarvis"`, `"name":"Jarvis"`, `"chat_id":"oc_group"`, `"open_id":"ou_assigner"`,
		`"source_messages":[{"message_id":"om_1","chat_id":"oc_group","sender_open_id":"ou_sender_1","sender_name":"发送人一","content":"完整源消息正文必须进入执行简报","create_time":1785985200}`,
		`{"message_id":"om_2","chat_id":"oc_group","sender_open_id":"ou_sender_2","sender_name":"发送人二","content":"另一条完整正文也必须进入","create_time":1785985260}`,
		`"background_lookup":"jarvis-tools get-task --id 97"`, `"source_quote":"请压缩上下文"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{
		`"background":`, "完整个人背景不应进执行简报", "完整项目决策不应进执行简报",
		"完整群背景不应进执行简报", "重复引用也不应重复输出",
		"完整 conversation 不应进执行简报", "完整 fact 不应进执行简报",
		"其它 Todo 不应进执行简报", "其它 Task 摘要不应进执行简报",
	} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("execution prompt leaked %q:\n%s", unwanted, prompt)
		}
	}
}

// TestBuildExecutionPromptForwardsSourcePayloadVerbatim pins the unified
// anti-goal-drift path for every Task source.
func TestBuildExecutionPromptForwardsSourcePayloadVerbatim(t *testing.T) {
	clue := `{"action_type":"manual_followup","desired_outcome":"产出这场会的结论并生成落到我身上的待办","semantics":"当前妙记无 view 权限，需先申请"}`
	task := &domain.Task{
		ID: 13, Title: "公会基建 Agent 日会会后处理", ActionType: "manual_followup",
		Target:        "公会基建Agent 日会（meeting_id=7667030332496007223）",
		SourcePayload: datatypes.JSON(clue),
		Background:    datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	prompt, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"source_payload":` + clue,
		`"target_hint":"公会基建Agent 日会（meeting_id=7667030332496007223）"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildExecutionPromptForTodoTaskWithNonSnapshotBackground(t *testing.T) {
	task := &domain.Task{
		ID: 18, Title: "事实维护任务", ActionType: "fact_update", Target: "世界事实抽取",
		SourcePayload: datatypes.JSON(`{"desired_outcome":"抽取这波事实并更新世界模型"}`),
		Background:    datatypes.JSON(`{"desired_outcome":"抽取这波事实并更新世界模型"}`), SourceType: "todo",
	}
	prompt, err := buildExecutionPrompt(testM5SystemPrompt, "修改文件需要审批。", task, "/workspace/jarvis", testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"source_payload":{"desired_outcome":"抽取这波事实并更新世界模型"}`,
		`"background_lookup":"jarvis-tools get-task --id 18"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestRepositoryM5PromptOwnsGoalAndExecution(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "conf", "prompts", "m5-system-prompt.md"))
	if err != nil {
		t.Fatalf("read repository M5 prompt: %v", err)
	}
	prompt := string(content)
	for _, want := range []string{
		"M5 是真正理解任务、调查事实、确定目标、选择动作、执行并验证结果的阶段",
		"`title_hint` 和 `target_hint`",
		"可以基于证据修改、替换或放弃这些建议",
		"根据调查持续重规划",
		"本身不代表任务完成",
		// A cleared blocker must never read as a finished goal; see
		// docs/design-long-horizon-agent-goal-control.md.
		"解除阻塞不是完成",
		"不得假设其中存在固定 JSON 字段",
		"以 source_payload 表达的真实最终结果为准",
		"完整冻结背景仍保存在 Task.background",
		"只有当前判断确实缺少某一类信息时才查",
		"不要自己发送审批卡片",
		"先把 proposal 和 `awaiting_approval` 状态持久化",
		"绑定当前 Task version",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("M5 prompt missing autonomy contract %q:\n%s", want, prompt)
		}
	}
	for _, obsolete := range []string{
		"严格执行 plan 的目标",
		"高风险、对外承诺、删改线上或后果说不清时，只发「查看详情」",
		"同意/拒绝/查看详情",
		"发出通知后在 effects[] 申报",
	} {
		if strings.Contains(prompt, obsolete) {
			t.Fatalf("M5 prompt still contains obsolete upstream constraint %q:\n%s", obsolete, prompt)
		}
	}
}

func stringPtr(value string) *string { return &value }

func uint64Ptr(value uint64) *uint64 { return &value }

func TestSummarizePriorRunsKeepsNewestOldestFirst(t *testing.T) {
	s1, s2, s3 := "first", "second", "third"
	t1 := time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 21, 2, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 7, 21, 3, 0, 0, 0, time.UTC)
	// ListRuns order: newest first.
	items := []RunView{
		{ID: 3, Status: "succeeded", Summary: &s3, StartedAt: t3},
		{ID: 2, Status: "failed", Summary: &s2, StartedAt: t2},
		{ID: 1, Status: "succeeded", Summary: &s1, StartedAt: t1},
	}
	got := summarizePriorRuns(items, 2)
	if len(got) != 2 || got[0].RunID != 2 || got[1].RunID != 3 {
		t.Fatalf("summarizePriorRuns = %#v, want oldest-first of newest 2 (2 then 3)", got)
	}
}

func TestDecodeExecutionSupplementsRejectsInvalidJSON(t *testing.T) {
	if _, err := decodeExecutionSupplements([]byte(`{"note":"x"}`)); err == nil {
		t.Fatalf("invalid supplements JSON must fail")
	}
}
