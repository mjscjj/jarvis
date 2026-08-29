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
	if err := run([]string{"configure-principal", "--config", configPath, "--agent-name", "小贾", "--open-id", "ou_test", "--git-author", "test@example.com"}, &output); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode output %q: %v", output.String(), err)
	}
	if got["agent_display_name"] != "小贾" || got["principal_open_id"] != "ou_test" || got["git_author"] != "test@example.com" ||
		got["card_approval_configured"] != true || got["relay_secret_configured"] != true || got["restart_required"] != true {
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
	if _, err := config.ConfigurePrincipal(configPath, "小贾", "ou_test", "test@example.com"); err != nil {
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
	if got["agent_display_name"] != "小贾" || got["principal_open_id"] != "ou_test" || got["git_author"] != "test@example.com" ||
		got["card_approval_enabled"] != true ||
		got["card_approval_principal_open_id"] != "ou_test" || got["relay_secret"] == "" || len(got["relay_secret_sha256"].(string)) != 64 {
		t.Fatalf("run() output = %#v", got)
	}
}

func TestRunInitializationStatusDoesNotExposeValues(t *testing.T) {
	repoConfig := filepath.Join("..", "..", "conf", "config.yaml")
	base, err := os.ReadFile(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, base, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.ConfigurePrincipal(configPath, "小贾", "ou_status_secret", "status-secret@example.com"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"initialization-status", "--config", configPath}, &output); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if bytes.Contains(output.Bytes(), []byte("ou_status_secret")) || bytes.Contains(output.Bytes(), []byte("status-secret@example.com")) {
		t.Fatalf("initialization status leaked identity values: %s", output.String())
	}
	var got struct {
		Ready              bool     `json:"machine_configuration_ready"`
		RuntimeExists      bool     `json:"runtime_config_exists"`
		TrackedModelAPIKey bool     `json:"tracked_model_api_key_present"`
		RuntimeBinaries    []string `json:"runtime_binaries"`
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Ready || !got.RuntimeExists || !got.TrackedModelAPIKey {
		t.Fatalf("initialization status = %#v", got)
	}
	if len(got.RuntimeBinaries) == 0 {
		t.Fatalf("initialization status omitted configured runtime binaries: %#v", got)
	}
}
