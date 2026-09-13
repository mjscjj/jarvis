package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type testAvailability map[string]bool

func (a testAvailability) SkillEnabled(_ context.Context, name string) (bool, error) {
	enabled, owned := a[name]
	if !owned {
		return true, nil
	}
	return enabled, nil
}

func TestParseMetadata(t *testing.T) {
	meta, err := parseMetadata([]byte("---\nname: example\ndescription: example skill\n---\n\n# Body\n"))
	if err != nil {
		t.Fatalf("parseMetadata() error = %v", err)
	}
	if meta.Name != "example" || meta.Description != "example skill" {
		t.Fatalf("metadata = %#v", meta)
	}
}

func TestNormalizeStages(t *testing.T) {
	stages, err := normalizeStages([]string{StageExecute, StageExtract, StageExecute})
	if err != nil {
		t.Fatalf("normalizeStages() error = %v", err)
	}
	if !reflect.DeepEqual(stages, []string{StageExtract, StageExecute}) {
		t.Fatalf("stages = %#v", stages)
	}
	if _, err := normalizeStages(nil); err == nil {
		t.Fatal("normalizeStages(nil) must fail")
	}
	if _, err := normalizeStages([]string{"unknown"}); err == nil {
		t.Fatal("normalizeStages(unknown) must fail")
	}
}

func TestServiceReadsAndUpdatesConfiguration(t *testing.T) {
	root, configPath := writeSkillFixture(t, false)
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	items, err := service.List(t.Context())
	if err != nil || len(items) != 1 || items[0].Name != "example" {
		t.Fatalf("List() = %#v err=%v", items, err)
	}
	enabled := false
	updated, err := service.Update(t.Context(), "example", Input{
		Stages: []string{StageExtract, StageExecute}, IsEnabled: &enabled,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.IsEnabled || !reflect.DeepEqual(updated.Stages, []string{StageExtract, StageExecute}) {
		t.Fatalf("Update() = %#v", updated)
	}
	reloaded, err := NewService(root, configPath)
	if err != nil {
		t.Fatalf("reload service: %v", err)
	}
	items, err = reloaded.List(t.Context())
	if err != nil || items[0].IsEnabled {
		t.Fatalf("reloaded List() = %#v err=%v", items, err)
	}
}

func TestDisabledCatalogEntryRemainsReadable(t *testing.T) {
	for _, inline := range []bool{false, true} {
		root, configPath := writeSkillFixture(t, inline)
		service, err := NewService(root, configPath)
		if err != nil {
			t.Fatal(err)
		}
		enabled := false
		if _, err := service.Update(t.Context(), "example", Input{Stages: []string{StageExtract}, IsEnabled: &enabled}); err != nil {
			t.Fatal(err)
		}
		catalog, err := service.Catalog(t.Context(), StageExtract)
		if err != nil || catalog != "" {
			t.Fatalf("disabled catalog=%q err=%v", catalog, err)
		}
		content, err := service.Content(t.Context(), "example")
		if err != nil || !strings.Contains(content.Content, "BODY_MARKER") {
			t.Fatalf("content=%v err=%v", content, err)
		}
	}
}

func TestAvailabilityGateHidesSkill(t *testing.T) {
	root, configPath := writeSkillFixture(t, false)
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	service.SetAvailability(testAvailability{"example": false})
	items, err := service.List(t.Context())
	if err != nil || len(items) != 1 || items[0].IsEnabled {
		t.Fatalf("List() = %#v err=%v", items, err)
	}
	if _, err := service.Content(t.Context(), "example"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Content() error = %v, want ErrNotFound", err)
	}
}

func TestRenderingServiceDoesNotChangeSource(t *testing.T) {
	root, configPath := writeSkillFixture(t, false)
	source, err := NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRenderingService(source, func(value string) string {
		return strings.ReplaceAll(value, "{{AGENT_NAME}}", "小贾")
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := service.Catalog(t.Context(), StageExecute)
	if err != nil {
		t.Fatal(err)
	}
	content, err := service.Content(t.Context(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(catalog, "小贾 skill") || !strings.Contains(content.Content, "# 小贾 body") {
		t.Fatalf("catalog=%q content=%q", catalog, content.Content)
	}
	raw, err := source.Content(t.Context(), "example")
	if err != nil || !strings.Contains(raw.Content, "{{AGENT_NAME}}") {
		t.Fatalf("source content changed: %#v err=%v", raw, err)
	}
}

func TestInlineSkillRendersBodyAndRespectsAvailability(t *testing.T) {
	root, configPath := writeSkillFixture(t, true)
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := service.Catalog(t.Context(), StageExtract)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BEGIN_INLINE_SKILL name=example", "# {{AGENT_NAME}} body", "BODY_MARKER"} {
		if !strings.Contains(catalog, want) {
			t.Fatalf("inline catalog missing %q:\n%s", want, catalog)
		}
	}
	service.SetAvailability(testAvailability{"example": false})
	catalog, err = service.Catalog(t.Context(), StageExtract)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(catalog, "BODY_MARKER") || strings.Contains(catalog, "name=example") {
		t.Fatalf("disabled inline skill leaked into catalog:\n%s", catalog)
	}
}

func TestServiceFailsWhenConfigurationDoesNotMatchFiles(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: missing\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(root, configPath); err == nil {
		t.Fatal("configured skill without a source file must fail")
	}
}

func writeSkillFixture(t *testing.T, inline bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	skillDirectory := filepath.Join(root, "example")
	if err := os.Mkdir(skillDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: example\ndescription: \"{{AGENT_NAME}} skill\"\n---\n\n# {{AGENT_NAME}} body\n\nBODY_MARKER\n"
	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	config := "skills:\n  - name: example\n    enabled: true\n    stages: [extract, execute]\n"
	if inline {
		config = "skills:\n  - name: example\n    enabled: true\n    inline: true\n    stages: [extract]\n"
	}
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, configPath
}
