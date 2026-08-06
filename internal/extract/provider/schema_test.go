package provider

import (
	"strings"
	"testing"
)

func TestTodoExtractionJSONSchemaIsStrict(t *testing.T) {
	schema := TodoExtractionJSONSchema()
	if schema["additionalProperties"] != false {
		t.Fatal("root schema allows additional properties")
	}
	properties := schema["properties"].(map[string]any)
	candidates := properties["candidates"].(map[string]any)
	candidate := candidates["items"].(map[string]any)
	if candidate["additionalProperties"] != false {
		t.Fatal("candidate schema allows additional properties")
	}
	// Every property must be required so the model boundary rejects omissions
	// instead of Go guessing them.
	if len(candidate["required"].([]string)) != len(candidate["properties"].(map[string]any)) {
		t.Fatal("not every candidate property is required")
	}
	if _, ok := candidate["properties"].(map[string]any)["target"]; !ok {
		t.Fatal("candidate schema is missing target")
	}
	if _, ok := candidate["properties"].(map[string]any)["payload"]; !ok {
		t.Fatal("candidate schema is missing payload")
	}
	if _, ok := candidate["properties"].(map[string]any)["open_questions"]; ok {
		t.Fatal("candidate schema still exposes semantic projection open_questions")
	}
}

func TestTodoExtractionJSONSchemaDescribesAdmissionNotExecutionPlan(t *testing.T) {
	properties := TodoExtractionJSONSchema()["properties"].(map[string]any)
	candidate := properties["candidates"].(map[string]any)["items"].(map[string]any)
	fields := candidate["properties"].(map[string]any)
	status := fields["status"].(map[string]any)["description"].(string)
	payload := fields["payload"].(map[string]any)["description"].(string)
	for _, want := range []string{"Task 准入结论", "未闭环结果", "只是可能有用不能准入"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status description missing %q: %s", want, status)
		}
	}
	for _, want := range []string{"开放的准入简报", "当前责任人", "不要写执行计划"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload description missing %q: %s", want, payload)
		}
	}
	for _, forbidden := range []string{"候选路径", "最终要达成的现实结果"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("payload description still contains execution-stage requirement %q: %s", forbidden, payload)
		}
	}
}
