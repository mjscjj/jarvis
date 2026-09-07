package execute

import (
	"jarvis/internal/contextsnap"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
	"jarvis/internal/skill"
	"jarvis/internal/workrule"
)

func testExecutionPromptInput(systemPrompt, approvalPolicy string, task *domain.Task, repoPath, toolCatalog, sharedMemory, workRules, skills string, history *runHistory) executionPromptInput {
	return executionPromptInput{
		SystemPrompt: systemPrompt, ApprovalPolicy: approvalPolicy, Task: task,
		RepoPath: repoPath, ToolCatalog: toolCatalog, SharedMemory: sharedMemory,
		WorkRules: workRules, Skills: skills, History: history,
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
			SourcePayload: frozenTestContent(`{"request":"通知相关人"}`, string(background)),
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
		"普通一对一或群聊会话消息通过 `feishu-send-message` Skill",
		"面向多个独立收件人的通知广播通过 `feishu-broadcast` Skill",
		"feishu-send-message",
		"feishu-broadcast",
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
		SourcePayload: frozenTestContent(`{"steps":["send"]}`, `{}`), ExecutionSupplements: datatypes.JSON(supplements),
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
		SourcePayload: frozenTestContent(`{"steps":["send"]}`, `{}`)}
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
		SourcePayload: frozenTestContent(`{"steps":["send"]}`, `{}`)}
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
		SourcePayload: frozenTestContent(`{"steps":["send"]}`, `{}`)}
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

func TestBuildExecutionPromptKeepsOnlyUsefulTaskHints(t *testing.T) {
	task := &domain.Task{
		ID: 12, Title: "评测截图", ActionType: "notify_principal", Target: "评测截图影响面",
		SourcePayload: frozenTestContent(`{"request":"评测截图"}`, `{}`),
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

func TestBuildExecutionPromptKeepsTaskProjectBindingWithoutSnapshotProject(t *testing.T) {
	task := &domain.Task{
		ID: 21, Title: "无项目快照", ActionType: "manual_followup", Target: "目标",
		ProjectID:     uint64Ptr(44),
		SourcePayload: frozenTestContent(`{"desired_outcome":"做完"}`, `{}`),
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
		SourcePayload: frozenTestContent(clue, `{}`),
	}
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil))
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"source":` + clue,
		`"target_hint":"公会基建Agent 日会（meeting_id=7667030332496007223）"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
}

func stringPtr(value string) *string { return &value }

func uint64Ptr(value uint64) *uint64 { return &value }

func TestDecodeExecutionSupplementsRejectsInvalidJSON(t *testing.T) {
	if _, err := decodeExecutionSupplements([]byte(`{"note":"x"}`)); err == nil {
		t.Fatalf("invalid supplements JSON must fail")
	}
}
