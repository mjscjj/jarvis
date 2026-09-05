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
  profile: cli_legacy
  principal_open_id: ou_bot_scope
  relay_secret: secret
lark_cli:
  profile: cli_legacy
  rate_limit: 7
`), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ConfigurePrincipal(configPath, "小贾", "ou_new_principal", "new.user@example.com")
	if err != nil {
		t.Fatalf("ConfigurePrincipal() error = %v", err)
	}
	if result.RuntimeConfigPath != overridePath || result.AgentDisplayName != "小贾" || result.PrincipalOpenID != "ou_new_principal" ||
		result.GitAuthor != "new.user@example.com" ||
		!result.CardApprovalConfigured || !result.RelaySecretConfigured || !result.RestartRequired {
		t.Fatalf("ConfigurePrincipal() = %#v", result)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Identity.DisplayName != "小贾" || cfg.Extract.PrincipalOpenID != "ou_new_principal" ||
		cfg.DailyDigest.GitAuthor != "new.user@example.com" || cfg.LarkCLI.RateLimit != 7 {
		t.Fatalf("initialized config = extract:%q lark:%#v", cfg.Extract.PrincipalOpenID, cfg.LarkCLI)
	}
	if !cfg.CardApproval.Enabled ||
		cfg.CardApproval.PrincipalOpenID != "ou_new_principal" || cfg.CardApproval.RelaySecret != "secret" {
		t.Fatalf("card approval was not rebound to the principal while preserving its secret: %#v", cfg.CardApproval)
	}
	raw, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "# keep this local configuration") {
		t.Fatalf("runtime comment was not preserved:\n%s", raw)
	}
	if strings.Contains(string(raw), "profile:") || strings.Contains(string(raw), "cli_legacy") {
		t.Fatalf("legacy lark-cli profile fields were not removed:\n%s", raw)
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
		name      string
		agentName string
		openID    string
		gitAuthor string
	}{
		{name: "empty agent name", agentName: " ", openID: "ou_user", gitAuthor: "user@example.com"},
		{name: "bad open id", agentName: "小贾", openID: "user-1", gitAuthor: "user@example.com"},
		{name: "empty git author", agentName: "小贾", openID: "ou_user", gitAuthor: "  "},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ConfigurePrincipal(configPath, test.agentName, test.openID, test.gitAuthor); err == nil {
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
	if _, err := ConfigurePrincipal(configPath, "小贾", "ou_user", "user@example.com"); err == nil || !strings.Contains(err.Error(), "field unknown_section not found") {
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

func TestInspectInitializationReportsFreshAndConfiguredStates(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(runtimeSettingsTestYAML), 0o640); err != nil {
		t.Fatal(err)
	}
	fresh, err := InspectInitialization(configPath)
	if err != nil {
		t.Fatalf("InspectInitialization(fresh) error = %v", err)
	}
	if fresh.RuntimeConfigExists || fresh.MachineConfigurationReady || fresh.BaseConfigMode != "0640" {
		t.Fatalf("fresh status = %#v", fresh)
	}
	if got := strings.Join(fresh.RuntimeBinaries, ","); got != "codex,lark-cli,traex" {
		t.Fatalf("fresh runtime binaries = %q", got)
	}
	if _, err := ConfigurePrincipal(configPath, "小贾", "ou_ready", "ready@example.com"); err != nil {
		t.Fatal(err)
	}
	configured, err := InspectInitialization(configPath)
	if err != nil {
		t.Fatalf("InspectInitialization(configured) error = %v", err)
	}
	if !configured.RuntimeConfigExists || configured.RuntimeConfigMode != "0600" ||
		!configured.AgentNameConfigured || !configured.MachineConfigurationReady {
		t.Fatalf("configured status = %#v", configured)
	}
}
