package provider

import "testing"

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
	slots := candidate["properties"].(map[string]any)["slots"].(map[string]any)
	if slots["additionalProperties"] != false {
		t.Fatal("slots schema allows additional properties")
	}
	if len(slots["required"].([]string)) != len(slots["properties"].(map[string]any)) {
		t.Fatal("not every slot property is required")
	}
}
