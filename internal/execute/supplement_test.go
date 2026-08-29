package execute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
	"jarvis/internal/skill"
	"jarvis/internal/workrule"
)

func testExecutionPromptInput(systemPrompt, approvalPolicy string, task *domain.Task, repoPath, toolCatalog, sharedMemory, workRules, skills string, previousRuns []priorRunSummary) executionPromptInput {
	return executionPromptInput{
		SystemPrompt: systemPrompt, ApprovalPolicy: approvalPolicy, Task: task,
		RepoPath: repoPath, ToolCatalog: toolCatalog, SharedMemory: sharedMemory,
		WorkRules: workRules, Skills: skills, PreviousRuns: previousRuns,
	}
}

const (
	testToolCatalog    = "BEGIN_AVAILABLE_TOOLS\n- fixture-tool\nEND_AVAILABLE_TOOLS"
	testM5SystemPrompt = "test M5 system prompt\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}"
)

func TestRepositoryM5EffectivePromptUsesExplicitMessageTool(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	read := func(relative string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(repoRoot, relative))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		return string(content)
	}
	rules, err := workrule.NewService(filepath.Join(repoRoot, "conf", "rules"))
	if err != nil {
		t.Fatalf("load repository work rules: %v", err)
	}
	ruleBlock, err := rules.Block(t.Context(), workrule.StageExecute)
	if err != nil {
		t.Fatalf("render repository M5 rules: %v", err)
	}
	skills, err := skill.NewService(
		filepath.Join(repoRoot, ".agents", "skills"),
		filepath.Join(repoRoot, "conf", "skills.yaml"),
	)
	if err != nil {
		t.Fatalf("load repository skills: %v", err)
	}
	skillCatalog, err := skills.Catalog(t.Context(), skill.StageExecute)
	if err != nil {
		t.Fatalf("render repository execute skills: %v", err)
	}
	background, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		Principal:       &contextsnap.Principal{OpenID: "ou_me", Name: "我"},
	}).Encode()
	if err != nil {
		t.Fatalf("encode prompt fixture: %v", err)
	}
	prompt, err := buildExecutionPrompt(executionPromptInput{
		SystemPrompt:   read("conf/prompts/m5-system-prompt.md"),
		ApprovalPolicy: read("conf/prompts/m5-approval-policy.md"),
		Task: &domain.Task{
			ID: 1, Title: "通知相关人", ActionType: "agent_task",
			SourcePayload: datatypes.JSON([]byte(`{"request":"通知相关人"}`)),
			Background:    datatypes.JSON(background),
		},
		ToolCatalog: testToolCatalog,
		WorkRules:   ruleBlock,
		Skills:      skillCatalog,
	})
	if err != nil {
		t.Fatalf("build repository M5 prompt: %v", err)
	}
	for _, want := range []string{
		"普通业务消息：显式工具动作",
		"先完整确定受众、会话位置或原消息锚点",
		"所有普通业务消息都通过 `feishu-send-message` Skill",
		"feishu-send-message",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("effective M5 prompt missing message-tool contract %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "user_message") {
		t.Fatalf("effective M5 prompt still contains legacy implicit message field:\n%s", prompt)
	}
}

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
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil))
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
	empty, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil))
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if strings.Contains(empty, "BEGIN_SHARED_MEMORY") {
		t.Fatalf("empty shared memory must not inject block:\n%s", empty)
	}
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "lark-cli 的 token 存在 ~/.lark 里", "", "", nil))
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
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "BEGIN_WORK_RULES\n- 禁止直接私聊\nEND_WORK_RULES", "", nil))
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
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "BEGIN_AVAILABLE_SKILLS\n- feishu-send-message\nEND_AVAILABLE_SKILLS", nil))
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
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", prior))
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
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil))
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

// TestBuildExecutionPromptCarriesFrozenBackgroundWhole pins that the snapshot
// M3 froze reaches M5 intact. Reshaping it here would hand M5 a different world
// than the one the Todo was admitted against.
func TestBuildExecutionPromptCarriesFrozenBackgroundWhole(t *testing.T) {
	projectCode := "jarvis"
	groupName := "公会 AI 突击群"
	assignerName := "测试委托人"
	assignerRole := "leader"
	snapshot, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		CapturedAt:      "2026-08-06T03:00:00Z",
		Principal:       &contextsnap.Principal{OpenID: "ou_principal", Name: "principal", Summary: stringPtr("principal 的长期事实页")},
		Project: &contextsnap.Project{
			ID: 7, Code: &projectCode, Name: "Jarvis", Role: "owner", Status: "active",
			Summary: stringPtr("项目的长期事实页"),
		},
		Group: &contextsnap.Group{
			ID: 9, ChatID: "oc_group", Name: &groupName,
			Description: stringPtr("群公告原文"), Summary: stringPtr("群的长期事实页"),
		},
		Assigner: &contextsnap.Assigner{OpenID: "ou_assigner", Name: &assignerName, Role: &assignerRole},
		Messages: []contextsnap.Message{
			{MessageID: "om_1", ChatID: "oc_group", SenderOpenID: "ou_sender_1", SenderName: "发送人一", Content: "完整源消息正文必须进入执行简报", CreateTime: 1785985200},
			{MessageID: "om_2", ChatID: "oc_group", SenderOpenID: "ou_sender_2", SenderName: "发送人二", Content: "另一条完整正文也必须进入", CreateTime: 1785985260},
		},
		Conversation: []contextsnap.Message{{MessageID: "om_context", Content: "周边对话也一起冻结"}},
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
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "/workspace/jarvis", testToolCatalog, "", "", "", nil))
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"current_status":"waiting"`, `"current_summary":"已经定位背景缺失，等待补齐 M5 首轮上下文"`,
		`"last_progress_at":"2026-08-06T04:05:00Z"`, `"project_id":7`, `"source_quote":"请压缩上下文"`,
		// The snapshot rides through whole, summaries and long tail included.
		`"execution_context":{`, "principal 的长期事实页", "项目的长期事实页", "群的长期事实页",
		"完整源消息正文必须进入执行简报", "另一条完整正文也必须进入",
		"周边对话也一起冻结",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "background_lookup") {
		t.Fatalf("execution prompt still points at a background lookup:\n%s", prompt)
	}
}

// TestBuildExecutionPromptKeepsTaskProjectBindingWithoutSnapshotProject covers
// the Task whose snapshot carries no project: the binding is a Task field, so it
// has to survive on the task object.
func TestBuildExecutionPromptKeepsTaskProjectBindingWithoutSnapshotProject(t *testing.T) {
	task := &domain.Task{
		ID: 21, Title: "无项目快照", ActionType: "manual_followup", Target: "目标",
		ProjectID:     uint64Ptr(44),
		SourcePayload: datatypes.JSON(`{"desired_outcome":"做完"}`),
		Background:    datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil))
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !strings.Contains(prompt, `"project_id":44`) {
		t.Fatalf("execution prompt lost the Task project binding:\n%s", prompt)
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
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil))
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

func TestBuildExecutionPromptRejectsNonSnapshotBackground(t *testing.T) {
	task := &domain.Task{
		ID: 18, Title: "事实维护任务", ActionType: "fact_update", Target: "世界事实抽取",
		SourcePayload: datatypes.JSON(`{"desired_outcome":"抽取这波事实并更新世界模型"}`),
		Background:    datatypes.JSON(`{"desired_outcome":"抽取这波事实并更新世界模型"}`), SourceType: "todo",
	}
	_, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "/workspace/jarvis", testToolCatalog, "", "", "", nil))
	if err == nil {
		t.Fatal("build prompt: expected todo Task without a context snapshot to fail")
	}
	if !strings.Contains(err.Error(), "background invalid") {
		t.Fatalf("build prompt error = %v", err)
	}
}

func stringPtr(value string) *string { return &value }

func uint64Ptr(value uint64) *uint64 { return &value }

func TestSummarizePriorRunsKeepsEveryRunOldestFirst(t *testing.T) {
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
	got := summarizePriorRuns(items)
	if len(got) != 3 || got[0].RunID != 1 || got[1].RunID != 2 || got[2].RunID != 3 {
		t.Fatalf("summarizePriorRuns = %#v, want every run oldest-first (1, 2, 3)", got)
	}
}

func TestDecodeExecutionSupplementsRejectsInvalidJSON(t *testing.T) {
	if _, err := decodeExecutionSupplements([]byte(`{"note":"x"}`)); err == nil {
		t.Fatalf("invalid supplements JSON must fail")
	}
}
