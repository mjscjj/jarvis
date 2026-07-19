package domain

import "testing"

func TestCoreModels(t *testing.T) {
	t.Parallel()

	models := CoreModels()
	if got, want := len(models), 7; got != want {
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
	}
	want := []string{"project", "feishu_group", "person", "todo", "task", "resource", "scan_record"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("CoreModels()[%d] table = %q, want %q", i, got[i], want[i])
		}
	}
}
