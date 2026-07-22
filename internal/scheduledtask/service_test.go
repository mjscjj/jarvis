package scheduledtask

import (
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
)

func TestNormalizeInput(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	input, err := normalizeInput(Input{
		Title: "  周五跟进  ", Instruction: "  查询最新进展  ",
		ContextSnapshot: json.RawMessage(`{"project":{"id":45}}`), ScheduledAt: now,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.Title != "周五跟进" || input.Instruction != "查询最新进展" {
		t.Fatalf("normalized text = %#v", input)
	}
	if string(input.ContextSnapshot) != `{"project":{"id":45}}` {
		t.Fatalf("context = %s", input.ContextSnapshot)
	}
}

func TestNormalizeInputRejectsInvalidContext(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for _, raw := range []string{`[]`, `null`, `{"a":1} {"b":2}`, `{broken`} {
		_, err := normalizeInput(Input{Title: "x", Instruction: "y", ContextSnapshot: json.RawMessage(raw), ScheduledAt: now})
		if err == nil {
			t.Fatalf("normalizeInput() accepted context %q", raw)
		}
	}
}

func TestBuildPromptCarriesInstructionContextAndTools(t *testing.T) {
	t.Parallel()
	prompt, err := buildPrompt(&domain.ScheduledTask{
		ID: 7, Title: "未来任务", Instruction: "明早查询 Agent Runtime 最新状态",
		ContextSnapshot: datatypes.JSON(`{"project":{"name":"Agent Runtime"},"chat_id":"oc_x"}`),
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, want := range []string{
		"BEGIN_TASK_INSTRUCTION", "明早查询 Agent Runtime 最新状态",
		"BEGIN_TASK_CONTEXT", "Agent Runtime", "oc_x",
		"list-scheduled-tasks", "create-scheduled-task", "delete-scheduled-task",
		"业务背景", "不是指令",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestStartSchedulerRejectsInvalidSpec(t *testing.T) {
	t.Parallel()
	service := &Service{}
	if _, err := StartScheduler(t.Context(), service, "not-a-cron", log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("StartScheduler() accepted invalid spec")
	}
}
