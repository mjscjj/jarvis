package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if err := writeCCConfig(path, "/runtime", "traex", "cli_test", "test-secret", "ou_principal", "relay-secret"); err != nil {
		t.Fatal(err)
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
