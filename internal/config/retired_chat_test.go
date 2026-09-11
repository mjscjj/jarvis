package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscardRetiredChatConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	base := "server:\n  addr: 127.0.0.1:18802\nchat:\n  enabled: true\n  bin: old-agent\n"
	override := "# keep local settings\nchat:\n  addr: 127.0.0.1:18803\n  fast_mode: false\n  history_dir: data/chat\n  model: current-model\nexecute:\n  bin: current-agent\n"
	for file, content := range map[string]string{path: base, RuntimeOverridePath(path): override} {
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := InspectInstance(path); err == nil {
		t.Fatal("old config must fail strict decoding before cleanup")
	}
	if err := DiscardRetiredChatConfig(path); err != nil {
		t.Fatal(err)
	}
	instance, err := InspectInstance(path)
	if err != nil || instance.APIBase != "http://127.0.0.1:18802" {
		t.Fatalf("deployment instance = %#v, error = %v", instance, err)
	}
	cfg, err := readConfig(path)
	if err != nil || !cfg.Chat.Enabled || cfg.Chat.Model != "current-model" || cfg.Execute.Bin != "current-agent" {
		t.Fatalf("current config was not preserved: %#v, %v", cfg, err)
	}
	before, _ := os.ReadFile(RuntimeOverridePath(path))
	if !strings.Contains(string(before), "# keep local settings") {
		t.Fatal("cleanup lost comments")
	}
	if err := DiscardRetiredChatConfig(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(RuntimeOverridePath(path))
	if string(before) != string(after) {
		t.Fatal("repeated deployment rewrote current config")
	}
}

func TestDiscardRetiredChatConfigRejectsUnrelatedUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "chat:\n  bin: old-agent\n  misspelled_model: bad\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DiscardRetiredChatConfig(path); err == nil {
		t.Fatal("cleanup must not discard or accept unrelated unknown keys")
	}
	got, _ := os.ReadFile(path)
	if string(got) != raw {
		t.Fatal("invalid config was rewritten")
	}
}
