package domain

import "testing"

func TestCoreModels(t *testing.T) {
	t.Parallel()

	models := CoreModels()
	if got, want := len(models), 15; got != want {
		t.Fatalf("CoreModels() length = %d, want %d", got, want)
	}

	got := []string{
		models[0].(*Project).TableName(),
		models[1].(*Group).TableName(),
		models[2].(*Person).TableName(),
		models[3].(*Todo).TableName(),
		models[4].(*Task).TableName(),
		models[5].(*Resource).TableName(),
		models[6].(*ScanRecord).TableName(),
		models[7].(*PrincipalProfile).TableName(),
		models[8].(*ManagedResource).TableName(),
		models[9].(*SharedMemory).TableName(),
		models[10].(*WorkRule).TableName(),
		models[11].(*TextStorage).TableName(),
		models[12].(*AgentSkill).TableName(),
		models[13].(*DailyDigest).TableName(),
		models[14].(*ScheduledTask).TableName(),
	}
	want := []string{"project", "feishu_group", "person", "todo", "task", "resource", "scan_record", "principal_profile", "managed_resource", "shared_memory", "work_rule", "text_storage", "agent_skill", "daily_digest", "scheduled_task"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("CoreModels()[%d] table = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCaptureModels(t *testing.T) {
	t.Parallel()
	models := CaptureModels()
	if got, want := len(models), 2; got != want {
		t.Fatalf("CaptureModels() length = %d, want %d", got, want)
	}
	if got := models[0].(*Message).TableName(); got != "message" {
		t.Errorf("Message table = %q", got)
	}
	if got := models[1].(*Checkpoint).TableName(); got != "chat_checkpoint" {
		t.Errorf("Checkpoint table = %q", got)
	}
}

func TestExtractModels(t *testing.T) {
	t.Parallel()
	models := ExtractModels()
	if got, want := len(models), 2; got != want {
		t.Fatalf("ExtractModels() length = %d, want %d", got, want)
	}
	if got := models[0].(*TodoExtractWatermark).TableName(); got != "todo_extract_watermark" {
		t.Errorf("TodoExtractWatermark table = %q", got)
	}
	if got := models[1].(*TodoEvent).TableName(); got != "todo_event" {
		t.Errorf("TodoEvent table = %q", got)
	}
}

func TestDecideModels(t *testing.T) {
	t.Parallel()
	models := DecideModels()
	if got, want := len(models), 1; got != want {
		t.Fatalf("DecideModels() length = %d, want %d", got, want)
	}
	if got := models[0].(*DecisionAudit).TableName(); got != "decision_audit" {
		t.Errorf("DecisionAudit table = %q", got)
	}
}
