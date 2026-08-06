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
