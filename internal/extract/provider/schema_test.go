package provider

import (
	"encoding/json"
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

func TestTodoExtractionJSONSchemaIsMachineEnvelopeOnly(t *testing.T) {
	properties := TodoExtractionJSONSchema()["properties"].(map[string]any)
	candidates := properties["candidates"].(map[string]any)
	candidate := candidates["items"].(map[string]any)
	fields := candidate["properties"].(map[string]any)
	status := fields["status"].(map[string]any)["description"].(string)
	payload := fields["payload"].(map[string]any)["description"].(string)
	if !strings.Contains(status, "控制状态") || !strings.Contains(status, "系统提示词定义") {
		t.Fatalf("status description does not delegate semantics to the system prompt: %s", status)
	}
	if !strings.Contains(payload, "开放文本") || !strings.Contains(payload, "不解析或重写") {
		t.Fatalf("payload description is not an opaque transport contract: %s", payload)
	}
	if description := candidates["description"].(string); !strings.Contains(description, "没有候选时使用空数组") {
		t.Fatalf("candidates description does not define the empty representation: %s", description)
	}

	encoded, err := json.Marshal(TodoExtractionJSONSchema())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{
		"闲聊", "值得启动", "未闭环", "只是可能有用", "需要 Principal", "需要 Jarvis",
		"准入结论", "当前责任人", "执行计划", "候选方案", "最终完成标准",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("machine schema contains admission semantic %q: %s", forbidden, text)
		}
	}
}
