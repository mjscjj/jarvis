package execute

import (
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
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
		Plan: datatypes.JSON(`{"steps":["send"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
		ExecutionSupplements: datatypes.JSON(supplements),
	}
	prompt, err := buildExecutionPrompt(task, "", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !strings.Contains(prompt, "执行阶段补充") || !strings.Contains(prompt, "标题要包含季度") {
		t.Fatalf("prompt missing supplements: %s", prompt)
	}
}

func TestBuildExecutionPromptIncludesPreviousRuns(t *testing.T) {
	task := &domain.Task{
		ID: 10, Title: "告诉唐建科 PSM", ActionType: "investigate",
		Plan: datatypes.JSON(`{"steps":["reply"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	finished := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	summary := "已建群并解释 PSM 是 Product-Service-Module"
	prior := []priorRunSummary{{
		RunID: 3, Status: "succeeded", Summary: summary,
		StartedAt: "2026-07-21T07:55:00Z", FinishedAt: finished.Format(time.RFC3339),
	}}
	prompt, err := buildExecutionPrompt(task, "", prior)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	for _, want := range []string{
		`"previous_runs"`, `"run_id":3`, summary, "不要重复做", "task-exec-v3",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
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
