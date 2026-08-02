package insight

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMyDayJSONUsesTaskCreationContract(t *testing.T) {
	raw, err := json.Marshal(MyDay{Date: "2026-08-02", TodosCreated: 1, TasksCreated: 2})
	if err != nil {
		t.Fatalf("marshal MyDay: %v", err)
	}
	encoded := string(raw)
	if !strings.Contains(encoded, `"tasks_created":2`) {
		t.Fatalf("MyDay JSON missing tasks_created: %s", encoded)
	}
	if strings.Contains(encoded, "confirmed") {
		t.Fatalf("MyDay JSON still exposes confirmation contract: %s", encoded)
	}
}

func TestDigestPromptDescribesTaskCreationWithoutConfirmationGate(t *testing.T) {
	digest := &Digest{Days: 1, Mine: []MyDay{{
		Date: "2026-08-02", TodosCreated: 1, TasksCreated: 2, TasksDone: 3, TasksFailed: 4,
	}}}
	prompt := buildDigestPrompt(digest)
	if !strings.Contains(prompt, "生成任务 2") {
		t.Fatalf("digest prompt missing task creation count:\n%s", prompt)
	}
	if strings.Contains(prompt, "确认生成任务") {
		t.Fatalf("digest prompt still describes a confirmation gate:\n%s", prompt)
	}
}
