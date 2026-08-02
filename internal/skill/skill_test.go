package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseMetadata(t *testing.T) {
	meta, err := parseMetadata([]byte("---\nname: feishu-send-message\ndescription: 发送飞书消息\n---\n\n# 正文\n"))
	if err != nil {
		t.Fatalf("parseMetadata() error = %v", err)
	}
	if meta.Name != "feishu-send-message" || meta.Description != "发送飞书消息" {
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
	if _, err := normalizeStages([]string{"M6"}); err == nil {
		t.Fatal("normalizeStages(unknown) must fail")
	}
}

func TestServiceReadsAndUpdatesYAMLConfiguration(t *testing.T) {
	root := t.TempDir()
	skillDirectory := filepath.Join(root, "feishu-send-message")
	if err := os.Mkdir(skillDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	skillText := "---\nname: feishu-send-message\ndescription: 发送飞书消息\n---\n\n# 正文\n"
	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte(skillText), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: feishu-send-message\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	items, err := service.List(t.Context())
	if err != nil || len(items) != 1 || items[0].Name != "feishu-send-message" {
		t.Fatalf("List() = %#v err=%v", items, err)
	}
	enabled := false
	updated, err := service.Update(t.Context(), "feishu-send-message", Input{
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

func TestServiceFailsWhenSkillConfigurationDoesNotMatchFiles(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: missing-skill\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(root, configPath); err == nil {
		t.Fatal("stale configured skill must fail")
	}
}
