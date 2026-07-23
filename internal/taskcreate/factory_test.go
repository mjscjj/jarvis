package taskcreate

import (
	"encoding/json"
	"testing"
)

func TestNormalizeInputDefaultsTodoSourceID(t *testing.T) {
	todoID := uint64(42)
	input, err := normalizeInput(Input{
		TodoID: &todoID, Title: "执行任务", ActionType: "investigate", Target: "目标",
		Background:  json.RawMessage(`{"snapshot_version":"v1"}`),
		Plan:        json.RawMessage(`{"instruction":"查清问题"}`),
		ConfirmedBy: "user", SourceType: SourceTodo, ExecutionMode: ExecutionModeStandard,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.SourceID == nil || *input.SourceID != todoID {
		t.Fatalf("source_id = %v, want %d", input.SourceID, todoID)
	}
}

func TestNormalizeInputRequiresScheduledOccurrence(t *testing.T) {
	sourceID := uint64(5)
	_, err := normalizeInput(Input{
		Title: "定时任务", ActionType: "agent_task", Target: "会议",
		Background:  json.RawMessage(`{"meeting_number":"123"}`),
		Plan:        json.RawMessage(`{"instruction":"加入会议"}`),
		ConfirmedBy: "scheduled_task", SourceType: SourceScheduledTask, SourceID: &sourceID,
		ExecutionMode: ExecutionModeDirect,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted scheduled source without occurrence_key")
	}
}

func TestNormalizeInputAcceptsEmptyBackgroundObject(t *testing.T) {
	input, err := normalizeInput(Input{
		Title: "无额外背景任务", ActionType: "agent_task", Target: "输出结论",
		Background:  json.RawMessage(`{}`),
		Plan:        json.RawMessage(`{"instruction":"输出结论"}`),
		ConfirmedBy: "user", SourceType: SourceManual, ExecutionMode: ExecutionModeDirect,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if string(input.Background) != `{}` {
		t.Fatalf("background = %s, want {}", input.Background)
	}
}

func TestNormalizeInputRejectsEmptyPlanObject(t *testing.T) {
	_, err := normalizeInput(Input{
		Title: "空计划任务", ActionType: "agent_task", Target: "输出结论",
		Background:  json.RawMessage(`{}`),
		Plan:        json.RawMessage(`{}`),
		ConfirmedBy: "user", SourceType: SourceManual, ExecutionMode: ExecutionModeDirect,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted empty plan")
	}
}

func TestActionHashCanonicalAndSensitive(t *testing.T) {
	first, err := ActionHash("agent_task", "会议", json.RawMessage(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatalf("ActionHash() error = %v", err)
	}
	second, err := ActionHash("agent_task", "会议", json.RawMessage(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatalf("ActionHash() canonical error = %v", err)
	}
	if first != second {
		t.Fatalf("canonical hashes differ: %s != %s", first, second)
	}
	changed, err := ActionHash("agent_task", "另一个会议", json.RawMessage(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatalf("ActionHash() changed error = %v", err)
	}
	if changed == first {
		t.Fatal("target change did not change action hash")
	}
}
