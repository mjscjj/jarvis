package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestRunShowConnectionUsesEffectiveConfigAndAddressOverride(t *testing.T) {
	// Reuse the config package's synthetic YAML rather than reading local
	// installation config, which may contain credentials.
	source, err := os.ReadFile(filepath.Join("..", "..", "internal", "config", "runtime_settings_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(string(source), "const runtimeSettingsTestYAML = `")
	if !ok {
		t.Fatal("synthetic config fixture not found")
	}
	body, _, ok = strings.Cut(body, "`")
	if !ok {
		t.Fatal("synthetic config fixture is incomplete")
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, overlay, address, apiBase, timezone string
		wantErr                                   bool
	}{
		{name: "base", apiBase: "http://127.0.0.1:18800", timezone: "Asia/Shanghai"},
		{name: "overlay", overlay: "server:\n  addr: '[::]:18802'\ncapture:\n  timezone: Asia/Tokyo\n", apiBase: "http://[::1]:18802", timezone: "Asia/Tokyo"},
		{name: "flag", overlay: "server:\n  addr: '0.0.0.0:18802'\ncapture:\n  timezone: UTC\n", address: "[::1]:18803", apiBase: "http://[::1]:18803", timezone: "UTC"},
		{name: "zero port", address: "127.0.0.1:0", wantErr: true},
		{name: "invalid timezone", overlay: "capture:\n  timezone: Missing/Zone\n", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overlay := tc.overlay
			if overlay == "" {
				overlay = "{}\n"
			}
			if err := os.WriteFile(config.RuntimeOverridePath(configPath), []byte(overlay), 0o600); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			args := []string{"show-connection", "--config", configPath}
			if tc.address != "" {
				args = append(args, "--addr", tc.address)
			}
			err := run(args, &output)
			if tc.wantErr {
				if err == nil || output.Len() != 0 {
					t.Fatalf("expected failure without output; got %q, %v", output.String(), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]string
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got["api_base"] != tc.apiBase || got["timezone"] != tc.timezone {
				t.Fatalf("show-connection output = %#v", got)
			}
		})
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
