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
	interval := 10
	input, nextRunAt, err := normalizeInput(Input{
		Title: "  周五跟进  ", Instruction: "  查询最新进展  ",
		ContextSnapshot: json.RawMessage(`{"project":{"id":45}}`),
		ScheduleType:    "interval", IntervalMinutes: &interval,
	}, now, now.Location())
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.Title != "周五跟进" || input.Instruction != "查询最新进展" {
		t.Fatalf("normalized text = %#v", input)
	}
	if string(input.ContextSnapshot) != `{"project":{"id":45}}` {
		t.Fatalf("context = %s", input.ContextSnapshot)
	}
	if !*input.Enabled || !nextRunAt.Equal(now.Add(10*time.Minute).UTC()) {
		t.Fatalf("enabled=%t next_run_at=%s", *input.Enabled, nextRunAt)
	}
}

func TestNormalizeInputRejectsInvalidContext(t *testing.T) {
	t.Parallel()
	now := time.Now()
	interval := 10
	for _, raw := range []string{`[]`, `null`, `{"a":1} {"b":2}`, `{broken`} {
		_, _, err := normalizeInput(Input{
			Title: "x", Instruction: "y", ContextSnapshot: json.RawMessage(raw),
			ScheduleType: "interval", IntervalMinutes: &interval,
		}, now, time.Local)
		if err == nil {
			t.Fatalf("normalizeInput() accepted context %q", raw)
		}
	}
}

func TestDailyAndIntervalNextOccurrence(t *testing.T) {
	t.Parallel()
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 7, 24, 9, 30, 0, 0, location)
	dailyTime := "09:00"
	_, dailyNext, err := normalizeInput(Input{
		Title: "daily", Instruction: "run", ScheduleType: "daily", DailyTime: &dailyTime,
	}, now, location)
	if err != nil {
		t.Fatalf("normalize daily input: %v", err)
	}
	wantDaily := time.Date(2026, 7, 25, 9, 0, 0, 0, location).UTC()
	if !dailyNext.Equal(wantDaily) {
		t.Fatalf("daily next = %s, want %s", dailyNext, wantDaily)
	}

	interval := 10
	oldNext := now.Add(-35 * time.Minute).UTC()
	intervalNext, err := nextOccurrence(&domain.ScheduledTask{
		ScheduleType: "interval", IntervalMinutes: &interval, NextRunAt: oldNext,
	}, now.UTC(), location)
	if err != nil {
		t.Fatalf("next interval occurrence: %v", err)
	}
	if want := now.Add(5 * time.Minute).UTC(); !intervalNext.Equal(want) {
		t.Fatalf("interval next = %s, want %s", intervalNext, want)
	}
}

func TestOneTimeNextOccurrenceAndFinalStatus(t *testing.T) {
	t.Parallel()
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 7, 24, 9, 30, 0, 0, location)
	runAt := now.Add(2 * time.Hour)
	input, nextRunAt, err := normalizeInput(Input{
		Title: "once", Instruction: "run", ScheduleType: "once", RunAt: &runAt,
	}, now, location)
	if err != nil {
		t.Fatalf("normalize once input: %v", err)
	}
	if input.DailyTime != nil || input.IntervalMinutes != nil || input.RunAt == nil {
		t.Fatalf("normalized once fields = %#v", input)
	}
	if !nextRunAt.Equal(runAt.UTC()) || !input.RunAt.Equal(runAt.UTC()) {
		t.Fatalf("once run_at=%v next_run_at=%v want=%v", input.RunAt, nextRunAt, runAt.UTC())
	}
	if got := finalTaskStatus("once"); got != "completed" {
		t.Fatalf("once final status = %q", got)
	}
	if got := finalTaskStatus("daily"); got != "active" {
		t.Fatalf("daily final status = %q", got)
	}
}

func TestNormalizeInputRejectsInvalidSchedule(t *testing.T) {
	t.Parallel()
	now := time.Now()
	badTime := "9:00"
	zero := 0
	for _, input := range []Input{
		{Title: "x", Instruction: "y", ScheduleType: "once"},
		{Title: "x", Instruction: "y", ScheduleType: "daily"},
		{Title: "x", Instruction: "y", ScheduleType: "daily", DailyTime: &badTime},
		{Title: "x", Instruction: "y", ScheduleType: "interval", IntervalMinutes: &zero},
		{Title: "x", Instruction: "y", ScheduleType: "cron"},
	} {
		if _, _, err := normalizeInput(input, now, time.Local); err == nil {
			t.Fatalf("normalizeInput() accepted %#v", input)
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
