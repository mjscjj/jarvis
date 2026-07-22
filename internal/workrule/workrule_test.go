package workrule

import (
	"strings"
	"testing"
)

func TestNormalizeInputAllCanonicalizesStages(t *testing.T) {
	enabled := true
	input, encoded, err := normalizeInput(Input{
		Name: " 通用规则 ", Content: " 先给结论 ", RuleType: RuleTypeAll,
		Stages: []string{StageExecute}, Priority: 10, IsEnabled: &enabled,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.Name != "通用规则" || input.Content != "先给结论" {
		t.Fatalf("input not trimmed: %+v", input)
	}
	if got := string(encoded); got != "[]" {
		t.Fatalf("all stages JSON = %s, want []", got)
	}
}

func TestNormalizeInputSelectedDeduplicatesAndOrders(t *testing.T) {
	input, encoded, err := normalizeInput(Input{
		Name: "发送规则", Content: "先建群", RuleType: RuleTypeSelected,
		Stages: []string{StageExecute, StageDecide, StageExecute}, Priority: 20,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if got := strings.Join(input.Stages, ","); got != "decide,execute" {
		t.Fatalf("stages = %q", got)
	}
	if got := string(encoded); got != `["decide","execute"]` {
		t.Fatalf("stages JSON = %s", got)
	}
}

func TestNormalizeInputRejectsEmptySelectedStages(t *testing.T) {
	_, _, err := normalizeInput(Input{Name: "x", Content: "y", RuleType: RuleTypeSelected, Priority: 1})
	if err == nil {
		t.Fatal("selected rule without stages must fail")
	}
}

func TestRenderBlockFiltersByStageAndEnabled(t *testing.T) {
	rules := []View{
		{Name: "通用", Content: "全部遵守", RuleType: RuleTypeAll, Priority: 10, IsEnabled: true},
		{Name: "只执行", Content: "执行遵守", RuleType: RuleTypeSelected, Stages: []string{StageExecute}, Priority: 20, IsEnabled: true},
		{Name: "禁用", Content: "不能出现", RuleType: RuleTypeAll, Priority: 30, IsEnabled: false},
	}
	extractBlock := RenderBlock(StageExtract, rules)
	if !strings.Contains(extractBlock, "通用") || strings.Contains(extractBlock, "只执行") || strings.Contains(extractBlock, "禁用") {
		t.Fatalf("unexpected extract block:\n%s", extractBlock)
	}
	executeBlock := RenderBlock(StageExecute, rules)
	for _, want := range []string{"BEGIN_WORK_RULES", "通用", "只执行", "当前阶段：execute"} {
		if !strings.Contains(executeBlock, want) {
			t.Fatalf("execute block missing %q:\n%s", want, executeBlock)
		}
	}
}
