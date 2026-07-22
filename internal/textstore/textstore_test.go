package textstore

import "testing"

func TestNormalizeInputPreservesArbitraryText(t *testing.T) {
	input, err := normalizeInput(Input{
		StorageKey: " custom_prompt ",
		Name:       " 自定义提示词 ",
		Content:    " 第一行\n第二行 ",
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.StorageKey != "custom_prompt" || input.Name != "自定义提示词" || input.Content != "第一行\n第二行" {
		t.Fatalf("normalizeInput() = %#v", input)
	}
}

func TestNormalizeInputRequiresAllFields(t *testing.T) {
	for _, input := range []Input{
		{Name: "name", Content: "content"},
		{StorageKey: "key", Content: "content"},
		{StorageKey: "key", Name: "name"},
	} {
		if _, err := normalizeInput(input); err == nil {
			t.Fatalf("normalizeInput(%#v) must fail", input)
		}
	}
}
