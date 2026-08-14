package execute

import (
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
