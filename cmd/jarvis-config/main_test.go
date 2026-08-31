package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/config"

	"gopkg.in/yaml.v3"
)

func TestRunAPIBaseUsesServerRuntimeConfig(t *testing.T) {
	base, err := os.ReadFile(filepath.Join("..", "..", "conf", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var baseline config.Config
	if err := yaml.Unmarshal(base, &baseline); err != nil {
		t.Fatal(err)
	}
	baseline.Extract.PrincipalOpenID = "ou_api_base_test"
	baseline.DailyDigest.GitAuthor = "api-base-test@example.com"
	base, err = yaml.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		runtime string
		want    string
		wantErr string
	}{
		{name: "baseline", want: "http://127.0.0.1:18800\n"},
		{name: "runtime port", runtime: "server:\n  addr: 0.0.0.0:18802\n", want: "http://127.0.0.1:18802\n"},
		{name: "other runtime setting", runtime: "server:\n  web_root: other-web\n", want: "http://127.0.0.1:18800\n"},
		{name: "ipv6", runtime: "server:\n  addr: '[::]:18803'\n", want: "http://[::1]:18803\n"},
		{name: "explicit host", runtime: "server:\n  addr: '192.168.1.2:18804'\n", want: "http://192.168.1.2:18804\n"},
		{name: "empty address", runtime: "server:\n  addr: ''\n", wantErr: "server.addr"},
		{name: "invalid address", runtime: "server:\n  addr: invalid\n", wantErr: "invalid server.addr"},
		{name: "unknown runtime key", runtime: "server:\n  unknown: 18802\n", wantErr: "parse runtime config override"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(configPath, base, 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.runtime != "" {
				if err := os.WriteFile(config.RuntimeOverridePath(configPath), []byte(tc.runtime), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			err := run([]string{"api-base", "--config", configPath}, &output)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || output.Len() != 0 {
					t.Fatalf("output = %q, error = %v, want error containing %q and no URL", output.String(), err, tc.wantErr)
				}
				return
			}
			if err != nil || output.String() != tc.want {
				t.Fatalf("output = %q, error = %v, want %q", output.String(), err, tc.want)
			}
		})
	}
}

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
	if err := run([]string{"configure-principal", "--config", configPath, "--open-id", "ou_test", "--git-author", "test@example.com"}, &output); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode output %q: %v", output.String(), err)
	}
	if got["principal_open_id"] != "ou_test" || got["git_author"] != "test@example.com" ||
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
	if _, err := config.ConfigurePrincipal(configPath, "ou_test", "test@example.com"); err != nil {
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
	if got["principal_open_id"] != "ou_test" || got["git_author"] != "test@example.com" ||
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
	if _, err := config.ConfigurePrincipal(configPath, "ou_status_secret", "status-secret@example.com"); err != nil {
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
