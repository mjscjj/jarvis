package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurePrincipalPreservesRuntimeOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(runtimeSettingsTestYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	overridePath := RuntimeOverridePath(configPath)
	if err := os.WriteFile(overridePath, []byte(`# keep this local configuration
card_approval:
  enabled: true
  profile: cli_bot
  principal_open_id: ou_bot_scope
  relay_secret: secret
lark_cli:
  rate_limit: 7
`), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ConfigurePrincipal(configPath, "ou_new_principal", "cli_new_user")
	if err != nil {
		t.Fatalf("ConfigurePrincipal() error = %v", err)
	}
	if result.RuntimeConfigPath != overridePath || result.PrincipalOpenID != "ou_new_principal" ||
		result.LarkProfile != "cli_new_user" || !result.RestartRequired {
		t.Fatalf("ConfigurePrincipal() = %#v", result)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Extract.PrincipalOpenID != "ou_new_principal" || cfg.LarkCLI.Profile != "cli_new_user" || cfg.LarkCLI.RateLimit != 7 {
		t.Fatalf("initialized config = extract:%q lark:%#v", cfg.Extract.PrincipalOpenID, cfg.LarkCLI)
	}
	if cfg.CardApproval.Profile != "cli_bot" || cfg.CardApproval.RelaySecret != "secret" {
		t.Fatalf("card approval was not preserved: %#v", cfg.CardApproval)
	}
	raw, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "# keep this local configuration") {
		t.Fatalf("runtime comment was not preserved:\n%s", raw)
	}
	info, err := os.Stat(overridePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("runtime config mode = %o, want 600", info.Mode().Perm())
	}
}

func TestConfigurePrincipalRejectsInvalidInputWithoutWriting(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(runtimeSettingsTestYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		openID  string
		profile string
	}{
		{name: "bad open id", openID: "user-1", profile: "cli_user"},
		{name: "empty profile", openID: "ou_user", profile: "  "},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ConfigurePrincipal(configPath, test.openID, test.profile); err == nil {
				t.Fatal("ConfigurePrincipal() succeeded")
			}
			if _, err := os.Stat(RuntimeOverridePath(configPath)); !os.IsNotExist(err) {
				t.Fatalf("runtime config was written after invalid input: %v", err)
			}
		})
	}
}

func TestConfigurePrincipalRejectsUnknownExistingRuntimeField(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(runtimeSettingsTestYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	overridePath := RuntimeOverridePath(configPath)
	original := []byte("unknown_section:\n  hidden: true\n")
	if err := os.WriteFile(overridePath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigurePrincipal(configPath, "ou_user", "cli_user"); err == nil || !strings.Contains(err.Error(), "field unknown_section not found") {
		t.Fatalf("ConfigurePrincipal() error = %v", err)
	}
	after, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("invalid runtime config changed:\n%s", after)
	}
}
