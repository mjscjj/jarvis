package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"jarvis/internal/config"
)

func TestRunConfigurePrincipal(t *testing.T) {
	repoConfig := filepath.Join("..", "..", "conf", "config.yaml")
	base, err := os.ReadFile(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, base, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"configure-principal", "--config", configPath, "--open-id", "ou_test", "--profile", "cli_test", "--git-author", "test@example.com"}, &output); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode output %q: %v", output.String(), err)
	}
	if got["principal_open_id"] != "ou_test" || got["lark_profile"] != "cli_test" || got["git_author"] != "test@example.com" || got["restart_required"] != true {
		t.Fatalf("run() output = %#v", got)
	}
}

func TestRunShowPrincipal(t *testing.T) {
	repoConfig := filepath.Join("..", "..", "conf", "config.yaml")
	base, err := os.ReadFile(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, base, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.ConfigurePrincipal(configPath, "ou_test", "cli_test", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"show-principal", "--config", configPath}, &output); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode output %q: %v", output.String(), err)
	}
	if got["principal_open_id"] != "ou_test" || got["lark_profile"] != "cli_test" || got["git_author"] != "test@example.com" {
		t.Fatalf("run() output = %#v", got)
	}
}
