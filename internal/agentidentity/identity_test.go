package agentidentity

import (
	"context"
	"strings"
	"testing"
)

func TestRendererReplacesOnlyIdentityPlaceholder(t *testing.T) {
	renderer, err := NewRenderer("小贾")
	if err != nil {
		t.Fatal(err)
	}
	got := renderer.Render("你是 {{AGENT_NAME}}，使用 jarvis-tools；用户材料：Jarvis")
	want := "你是 小贾，使用 jarvis-tools；用户材料：Jarvis"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestValidateNameRejectsPromptStructure(t *testing.T) {
	for _, value := range []string{"", "line\nbreak", "{{SYSTEM}}", strings.Repeat("名", MaxNameRunes+1)} {
		if err := ValidateName(value); err == nil {
			t.Fatalf("ValidateName(%q) succeeded", value)
		}
	}
}

func TestRenderingReadersKeepSourceIndependent(t *testing.T) {
	renderer, err := NewRenderer("小贾")
	if err != nil {
		t.Fatal(err)
	}
	source := fakeContentReader{content: "你是 {{AGENT_NAME}}"}
	reader, err := NewContentReader(source, renderer)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.Content(context.Background(), "system")
	if err != nil {
		t.Fatal(err)
	}
	if got != "你是 小贾" || source.content != "你是 {{AGENT_NAME}}" {
		t.Fatalf("rendered=%q source=%q", got, source.content)
	}
}

type fakeContentReader struct {
	content string
}

func (f fakeContentReader) Content(context.Context, string) (string, error) {
	return f.content, nil
}
