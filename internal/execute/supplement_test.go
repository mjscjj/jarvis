package execute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"

	"jarvis/internal/datatypes"
)

const testToolCatalog = "BEGIN_AVAILABLE_TOOLS\n- fixture-tool\nEND_AVAILABLE_TOOLS"

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
	prompt, err := buildExecutionPrompt("test M5 system prompt", "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil)
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
	empty, err := buildExecutionPrompt("test M5 system prompt", "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if strings.Contains(empty, "BEGIN_SHARED_MEMORY") {
		t.Fatalf("empty shared memory must not inject block:\n%s", empty)
	}
	prompt, err := buildExecutionPrompt("test M5 system prompt", "修改文件需要审批。", task, "", testToolCatalog, "lark-cli 的 token 存在 ~/.lark 里", "", "", nil)
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
	prompt, err := buildExecutionPrompt("test M5 system prompt", "修改文件需要审批。", task, "", testToolCatalog, "", "BEGIN_WORK_RULES\n- 禁止直接私聊\nEND_WORK_RULES", "", nil)
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
	prompt, err := buildExecutionPrompt("test M5 system prompt", "修改文件需要审批。", task, "", testToolCatalog, "", "", "BEGIN_AVAILABLE_SKILLS\n- feishu-send-message\nEND_AVAILABLE_SKILLS", nil)
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
	prompt, err := buildExecutionPrompt("test M5 system prompt", "修改文件需要审批。", task, "", testToolCatalog, "", "", "", prior)
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

func TestBuildExecutionPromptLabelsUpstreamSemanticsAsHints(t *testing.T) {
	task := &domain.Task{
		ID: 12, Title: "评测截图", ActionType: "notify_principal", Target: "评测截图影响面",
		Background: datatypes.JSON(`{"snapshot_version":"v1"}`), SourcePayload: datatypes.JSON(`{"request":"评测截图"}`),
	}
	prompt, err := buildExecutionPrompt("test M5 system prompt", "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"title_hint":"评测截图"`,
		`"action_type_hint":"notify_principal"`,
		`"target_hint":"评测截图影响面"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing hint field %q:\n%s", want, prompt)
		}
	}
	for _, obsolete := range []string{`"action_type":`, `"plan":`, `"decision_payload":`, `"decision_direction":`, `"decision_context":`} {
		if strings.Contains(prompt, obsolete) {
			t.Fatalf("execution prompt still exposes upstream semantics as authoritative field %q:\n%s", obsolete, prompt)
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
	prompt, err := buildExecutionPrompt("test M5 system prompt", "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil)
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

func TestRepositoryM5PromptOwnsGoalAndExecution(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "conf", "prompts", "m5-system-prompt.md"))
	if err != nil {
		t.Fatalf("read repository M5 prompt: %v", err)
	}
	prompt := string(content)
	for _, want := range []string{
		"M5 是真正理解任务、调查事实、确定目标、选择动作、执行并验证结果的阶段",
		"`title_hint`、`action_type_hint` 和 `target_hint`",
		"可以基于证据修改、替换或放弃这些建议",
		"根据调查持续重规划",
		"`action_type_hint` 不限制实际动作",
		"本身不代表任务完成",
		// A cleared blocker must never read as a finished goal; see
		// docs/design-long-horizon-agent-goal-control.md.
		"解除阻塞不是完成",
		"不得假设其中存在固定 JSON 字段",
		"以 source_payload 表达的真实最终结果为准",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("M5 prompt missing autonomy contract %q:\n%s", want, prompt)
		}
	}
	for _, obsolete := range []string{"严格执行 plan 的目标"} {
		if strings.Contains(prompt, obsolete) {
			t.Fatalf("M5 prompt still contains obsolete upstream constraint %q:\n%s", obsolete, prompt)
		}
	}
}

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
