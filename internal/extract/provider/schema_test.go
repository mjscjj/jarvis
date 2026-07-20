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
	// Every property must be required so the model boundary rejects omissions
	// instead of Go guessing them.
	if len(candidate["required"].([]string)) != len(candidate["properties"].(map[string]any)) {
		t.Fatal("not every candidate property is required")
	}
	if _, ok := candidate["properties"].(map[string]any)["target"]; !ok {
		t.Fatal("candidate schema is missing target")
	}
	if _, ok := candidate["properties"].(map[string]any)["open_questions"]; !ok {
		t.Fatal("candidate schema is missing open_questions")
	}
}
