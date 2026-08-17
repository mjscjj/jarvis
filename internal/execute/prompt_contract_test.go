package execute

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestM5PromptConsumesM3AdmissionWithoutRepeatingIt(t *testing.T) {
	raw, err := os.ReadFile("../../conf/prompts/m5-system-prompt.md")
	if err != nil {
		t.Fatalf("read M5 system prompt: %v", err)
	}
	system := string(raw)
	for _, want := range []string{
		"M3 已经完成了 Task 准入初筛",
		"不要从头重复一轮泛化价值判断",
		"先核验是否出现了让线索已经完成、失效、重复或转由他人负责的新事实",
		"准入仍成立时，直接调查",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("M5 system prompt missing %q", want)
		}
	}
	if strings.Contains(system, "先独立判断：这件事现在是否仍值得做") {
		t.Fatal("M5 system prompt still tells the agent to repeat M3 admission")
	}
}

func TestM5SystemPromptOwnsPhaseBehaviorAndStructuredFinalProtocol(t *testing.T) {
	raw, err := os.ReadFile("../../conf/prompts/m5-system-prompt.md")
	if err != nil {
		t.Fatalf("read M5 system prompt: %v", err)
	}
	system := string(raw)
	for _, want := range []string{
		"execute：先完成安全的只读调查",
		"apply：APPROVED_PROPOSAL 是 principal 已审阅的副作用内容",
		"resume_waiting：继续同一个 Session",
		"resume_human：继续同一个 Session",
		"仍只输出系统提供的结构化协议",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("M5 system prompt missing owned behavior %q", want)
		}
	}
	if strings.Contains(system, "最终只输出修改后的自然回答") {
		t.Fatal("M5 system prompt contains a natural-answer final protocol that conflicts with structured output")
	}
}

func TestM5SystemPromptAuthorizesReadTimeSummaryFixesWithBoundaries(t *testing.T) {
	raw, err := os.ReadFile("../../conf/prompts/m5-system-prompt.md")
	if err != nil {
		t.Fatalf("read M5 system prompt: %v", err)
	}
	system := string(raw)
	for _, want := range []string{
		"但读到已经写错的长期事实页时，就地改对",
		"只改你本轮亲自核实过的那部分结论",
		"必须带上刚 `get-page` 读到的 `updated_at`",
		"既不是需要审批的对外副作用",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("M5 system prompt missing read-time fix contract %q", want)
		}
	}
	if strings.Contains(system, "4. 长期事实不用你手工记") {
		t.Fatal("M5 system prompt still forbids all summary writes instead of authorizing read-time fixes")
	}
}

func TestRuntimeM5PhaseBlocksOnlyCarryPhaseState(t *testing.T) {
	for phase, block := range map[string]string{
		"execute":        m5PhaseExecute,
		"apply":          m5PhaseApply,
		"resume_waiting": m5PhaseResumeWaiting,
		"resume_human":   m5PhaseResumeHuman,
	} {
		want := "BEGIN_M5_PHASE\nphase=" + phase + "\nEND_M5_PHASE"
		if block != want {
			t.Fatalf("runtime phase %q contains stable behavior instead of only state:\n%s", phase, block)
		}
	}
}

func TestExecutionSchemaDoesNotOwnApprovalBehavior(t *testing.T) {
	for _, forbidden := range []string{"Return it with", "without performing", "outcome=needs_human", "complete proposal"} {
		if strings.Contains(executionResultSchema, forbidden) {
			t.Fatalf("execution schema contains approval behavior %q", forbidden)
		}
	}
	if !strings.Contains(executionResultSchema, "criteria are defined by APPROVAL_POLICY") {
		t.Fatal("execution schema does not delegate approval criteria to APPROVAL_POLICY")
	}
}

func TestM5SourceReplyHasOneOwner(t *testing.T) {
	systemRaw, err := os.ReadFile("../../conf/prompts/m5-system-prompt.md")
	if err != nil {
		t.Fatalf("read M5 system prompt: %v", err)
	}
	rulesRaw, err := os.ReadFile("../../conf/rules/m5.md")
	if err != nil {
		t.Fatalf("read M5 work rules: %v", err)
	}
	effective := string(systemRaw) + "\n" + string(rulesRaw)
	for _, want := range []string{
		"`user_message` 是 Task 来源会话里唯一面向用户的结果文案",
		"Jarvis 会尝试在来源消息上添加 `OnIt` reaction 表示已开始处理",
		"reaction 可能因为 Bot 尚未进入会话而失败，但不影响你继续完成 Task",
		"不要再调用 `feishu-send-message` 向这个来源会话发送同一 Task 的状态或答案",
		"`summary`、`progress_summary`、工具调用、消息 ID、自查依据和审计过程只留在 Task 内部",
	} {
		if !strings.Contains(effective, want) {
			t.Fatalf("effective M5 instructions missing source-reply ownership %q", want)
		}
	}
	if strings.Contains(effective, "任务做完了，成功或失败等状态需要同步") {
		t.Fatal("effective M5 instructions still contain the old direct-send rule")
	}
	for _, removed := range []string{"正在处理中", "原位更新"} {
		if strings.Contains(effective, removed) {
			t.Fatalf("effective M5 instructions still contain old progress-message behavior %q", removed)
		}
	}

	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(executionResultSchema), &schema); err != nil {
		t.Fatalf("decode executionResultSchema: %v", err)
	}
	found := false
	for _, field := range schema.Required {
		if field == "user_message" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("execution result schema does not require user_message")
	}
}
