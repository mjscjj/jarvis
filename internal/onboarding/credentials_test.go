package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"jarvis/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func credentialClient(t *testing.T, response string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" || r.Method != "POST" {
			t.Errorf("unexpected credential endpoint: %s %s", r.Method, r.URL)
		}
		var submitted map[string]string
		if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
			t.Fatal(err)
		}
		if submitted["app_id"] != "cli_test" || submitted["app_secret"] == "" {
			t.Error("missing submitted credentials")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
}

type commandFunc func(context.Context, string, []string, string) ([]byte, error)

func (f commandFunc) Run(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
	return f(ctx, bin, args, input)
}

func (f commandFunc) RunJSON(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
	return f(ctx, bin, args, input)
}

func TestInvalidCredentialsDoNotLeakUpstreamOutput(t *testing.T) {
	err := verifyAppCredentials(t.Context(), credentialClient(t, `{"code":10003,"msg":"secret echoed by upstream"}`), "cli_test", "new-secret")
	if err == nil || strings.Contains(err.Error(), "secret echoed") || strings.Contains(err.Error(), "new-secret") {
		t.Fatalf("unsafe or absent error: %v", err)
	}
}

func TestCredentialValidationFailsClosed(t *testing.T) {
	for _, response := range []string{`{}`, `{"code":0}`, `{"tenant_access_token":"token"}`, `not-json`, `{"code":1,"tenant_access_token":"token"}`} {
		if err := verifyAppCredentials(t.Context(), credentialClient(t, response), "cli_test", "test-secret"); err == nil {
			t.Errorf("accepted invalid response %q", response)
		}
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private output") })}
	if err := verifyAppCredentials(t.Context(), client, "cli_test", "test-secret"); err == nil || strings.Contains(err.Error(), "private output") {
		t.Fatalf("network error = %v", err)
	}
}

func TestExistingChannelSecretIsReusedWithoutLeakingToBrowser(t *testing.T) {
	service := &Service{options: Options{StateRoot: t.TempDir()}, runner: onboardingRunnerStub{}}
	path := filepath.Join(service.options.StateRoot, "cc-connect", "config.toml")
	runtimeRoot := t.TempDir()
	promptPath := filepath.Join(runtimeRoot, "conf", "prompts", "cc-system-prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, []byte("CC prompt at {{REPO_ROOT}}\ncreate-task source_type=manual delivery_required\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeCCConfig(path, runtimeRoot, "traex", "cli_test", "test-secret", "ou_principal", "relay-secret"); err != nil {
		t.Fatal(err)
	}
	configured, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configured), "CC prompt at "+runtimeRoot) || strings.Contains(string(configured), "{{REPO_ROOT}}") {
		t.Fatalf("CC prompt was not loaded and rendered from runtime: %s", configured)
	}
	secret, err := service.savedSecret("cli_test")
	if err != nil || secret != "test-secret" {
		t.Fatalf("saved credential unavailable: %v", err)
	}
	other, err := service.savedSecret("cli_other")
	if err != nil || other != "" {
		t.Fatal("reused another app's secret")
	}
	status := service.larkStatus(t.Context())
	if !status.CredentialAvailable {
		t.Fatal("saved credential not detected")
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "test-secret") || strings.Contains(string(raw), "test-token") {
		t.Fatal("credential leaked to browser")
	}
}

func TestFinalizeInvalidSecretDoesNotWriteConfiguration(t *testing.T) {
	root := t.TempDir()
	service := &Service{options: Options{ConfigPath: filepath.Join(root, "config.yaml"), StateRoot: root, HTTPClient: credentialClient(t, `{"code":1}`)}}
	service.runner = commandFunc(func(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
		if strings.Join(args, " ") == "login status" {
			return []byte("Logged in"), nil
		}
		return (onboardingRunnerStub{}).Run(ctx, bin, args, input)
	})
	_, err := service.Finalize(t.Context(), "", "test@example.com", "wrong-secret")
	if err == nil || !strings.Contains(err.Error(), "验证未通过") {
		t.Fatalf("Finalize = %v", err)
	}
	for _, path := range []string{config.RuntimeOverridePath(service.options.ConfigPath), filepath.Join(root, "cc-connect", "config.toml"), filepath.Join(root, "restart.requested")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unexpected write to %s", path)
		}
	}
}

func TestCancelFlowStopsCommandAndLateCompletionCannotSucceed(t *testing.T) {
	service := &Service{flows: map[string]*Flow{"flow-1": {ID: "flow-1", Status: flowPending}}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if !service.registerFlowCancel("flow-1", cancel) {
		t.Fatal("cancel registration failed")
	}
	if _, err := service.CancelFlow("flow-1"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("command was not cancelled")
	}
	service.finishFlow("flow-1", nil, nil)
	flow, err := service.Flow("flow-1")
	if err != nil || flow.Status != flowFailed {
		t.Fatal("late completion overwrote cancellation")
	}
	if service.registerFlowCancel("flow-1", cancel) {
		t.Fatal("cancelled flow started again")
	}
}

func TestRepairCredentialsPreservesExistingConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cc-connect", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := `# Personal configuration
language = "zh"
[[projects]]
name = "other"
custom = 42
[[projects.platforms]]
type = "feishu"
[projects.platforms.options]
app_id = "cli_other"
app_secret = "other-secret"
[[projects]]
name = "jarvis-codex"
custom = ["keep", "me"]
[projects.agent.options]
model = "my-model"
[[projects.platforms]]
type = "feishu"
[projects.platforms.options]
app_id = "cli_test"
app_secret = "old-secret"
allow_from = "ou_original"
jarvis_route_claim_secret = "relay-keep"
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{options: Options{StateRoot: root, HTTPClient: credentialClient(t, `{"code":0,"tenant_access_token":"test-token"}`)}}
	calls := 0
	service.runner = commandFunc(func(_ context.Context, _ string, args []string, input string) ([]byte, error) {
		calls++
		if strings.Join(args, " ") == "config show" {
			return []byte(`{"appId":"cli_test","profile":"existing-profile","brand":"feishu","lang":"zh"}`), nil
		}
		want := "config init --name existing-profile --app-id cli_test --brand feishu --app-secret-stdin --lang zh"
		if strings.Join(args, " ") != want || input != "new-secret\n" {
			t.Fatalf("incorrect credential update args=%v", args)
		}
		return []byte(`{"ok":true}`), nil
	})
	if err := service.RepairLarkCredentials(t.Context(), "new-secret"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual map[string]any
	if err := toml.Unmarshal([]byte(strings.Replace(original, `app_secret = "old-secret"`, `app_secret = "new-secret"`, 1)), &expected); err != nil {
		t.Fatal(err)
	}
	if err := toml.Unmarshal(raw, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("credential repair changed unrelated config values")
	}
	if !strings.Contains(string(raw), `app_secret = "new-secret"`) || !strings.Contains(string(raw), `name = "jarvis-codex"`) {
		t.Fatal("credential repair must preserve the double-quoted strings consumed by CC shell tools")
	}
	if calls != 2 {
		t.Fatalf("commands=%d", calls)
	}
	if _, err := os.Stat(filepath.Join(root, "restart.requested")); err != nil {
		t.Fatal("restart was not requested")
	}
	for _, name := range []string{"world-model.required", "config.runtime.yaml"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("repair created %s", name)
		}
	}
}

func TestRepairCredentialsRejectsBeforeWriting(t *testing.T) {
	for _, test := range []struct{ name, config, response string }{
		{"invalid secret", `[[projects]]
name="jarvis-codex"`, `{"code":1}`},
		{"invalid config", `not valid toml`, `{"code":0,"tenant_access_token":"test-token"}`},
		{"different app", `[[projects]]
name="jarvis-codex"
[[projects.platforms]]
type="feishu"
[projects.platforms.options]
app_id="cli_other"
app_secret="keep"`, `{"code":0,"tenant_access_token":"test-token"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "cc-connect", "config.toml")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.config), 0o600); err != nil {
				t.Fatal(err)
			}
			s := &Service{options: Options{StateRoot: root, HTTPClient: credentialClient(t, test.response)}, runner: commandFunc(func(_ context.Context, _ string, args []string, _ string) ([]byte, error) {
				if strings.Join(args, " ") != "config show" {
					t.Fatal("configuration mutated before validation")
				}
				return []byte(`{"appId":"cli_test","profile":"existing","brand":"feishu"}`), nil
			})}
			if err := s.RepairLarkCredentials(t.Context(), "invalid-or-new"); err == nil {
				t.Fatal("invalid repair succeeded")
			}
			raw, _ := os.ReadFile(path)
			if string(raw) != test.config {
				t.Fatal("original config was changed")
			}
		})
	}
}

func TestFailedLarkVerificationStillIdentifiesConfiguredApp(t *testing.T) {
	s := &Service{runner: commandFunc(func(_ context.Context, _ string, args []string, _ string) ([]byte, error) {
		if args[0] == "config" {
			return []byte(`{"appId":"cli_test","profile":"existing"}`), nil
		}
		return []byte(`{"error":{"message":"invalid app secret"}}`), errors.New("exit 1")
	})}
	status := s.larkStatus(t.Context())
	if status.AppID != "cli_test" || status.Error == "" || status.Bot.Verified {
		t.Fatalf("status=%+v", status)
	}
	if flow, err := s.BeginLarkSetup(t.Context()); err == nil || flow != nil {
		t.Fatalf("invalid configured credentials reported setup success: %v, %v", flow, err)
	}
}

func TestConfigShowUsesOnlyStdout(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "lark-cli")
	// Captured CLI 1.0.93 contract: JSON on stdout, config path on stderr.
	script := "#!/bin/sh\nprintf '%s\\n' '{\"appId\":\"cli_test\",\"profile\":\"default\",\"brand\":\"feishu\"}'\nprintf '\\nConfig file path: /tmp/config.json\\n' >&2\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Service{options: Options{LarkCLIBin: bin}, runner: execRunner{}}
	current, err := s.currentLarkConfig(t.Context())
	if err != nil || current.AppID != "cli_test" || current.Profile != "default" {
		t.Fatalf("read CLI config: %v, %v", current, err)
	}
}

func TestAgentLoginStatusReadsStderr(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'Logged in using Trae\\n' >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Service{options: Options{AgentCLIBin: bin}, runner: execRunner{}}
	status := s.agentStatus(t.Context())
	if !status.Authenticated || status.Error != "" {
		t.Fatalf("stderr login status: %+v", status)
	}
}
