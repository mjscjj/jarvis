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
	prompt, err := buildExecutionPrompt(task, "")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !strings.Contains(prompt, "执行阶段补充") || !strings.Contains(prompt, "标题要包含季度") {
		t.Fatalf("prompt missing supplements: %s", prompt)
	}
}

func TestDecodeExecutionSupplementsRejectsInvalidJSON(t *testing.T) {
	if _, err := decodeExecutionSupplements([]byte(`{"note":"x"}`)); err == nil {
		t.Fatalf("invalid supplements JSON must fail")
	}
}
