package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"
)

func TestConnectingExistingLarkAppNeverCreatesAnother(t *testing.T) {
	service := &Service{runner: commandFunc(func(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
		if bin == "bash" && len(args) == 3 && args[1] == "check" && filepath.Base(args[0]) == "jarvis-lark-auth" {
			return []byte(`{"ok":true}`), nil
		}
		if args[0] != "auth" {
			t.Fatal("attempted to reconfigure an existing app")
		}
		return (onboardingRunnerStub{}).Run(ctx, bin, args, input)
	})}
	flow, err := service.BeginLarkSetup(t.Context())
	if err != nil || flow.Status != flowSuccess {
		t.Fatalf("existing connection: %v, %v", flow, err)
	}
}

type setupRunner struct{ streamingRunnerStub }

func (setupRunner) RunStreaming(ctx context.Context, _ string, args []string, _ string, onOutput func([]byte)) ([]byte, error) {
	if strings.Join(args, " ") != "config init --new" {
		return nil, errors.New("wrong setup command")
	}
	onOutput([]byte("Open https://example.test/connect\nApp Secret: must-not-leak\n"))
	return []byte("must-not-leak"), nil
}

func TestAppCreationPublishesOnlyConnectionURL(t *testing.T) {
	service := &Service{runner: setupRunner{}, flows: map[string]*Flow{"new": {ID: "new", Status: flowPending}}}
	service.completeLarkSetup("new")
	flow, err := service.Flow("new")
	if err != nil {
		t.Fatal(err)
	}
	if flow.Status != flowSuccess || flow.VerificationURL != "https://example.test/connect" {
		t.Fatalf("flow: %#v", flow)
	}
	raw, err := json.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-not-leak") || flow.Output != "" {
		t.Fatal("raw credential output exposed")
	}
}

type unconfiguredSetupRunner struct{ setupRunner }

func (unconfiguredSetupRunner) Run(context.Context, string, []string, string) ([]byte, error) {
	// lark-cli 1.0.93 returns this error for an empty configuration directory.
	return []byte(`{"ok":false,"error":{"type":"config","subtype":"not_configured","message":"not configured"}}`), errors.New("exit status 3")
}

func TestUnconfiguredLarkCanStartFirstConnection(t *testing.T) {
	service := &Service{runner: unconfiguredSetupRunner{}, flows: make(map[string]*Flow)}
	status := service.larkStatus(t.Context())
	if !status.Available || status.AppID != "" || status.Error != "" {
		t.Fatalf("fresh installation should be ready to connect: %+v", status)
	}
	flow, err := service.BeginLarkSetup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if flow.Status != flowPending {
		t.Fatalf("connection status = %q, want pending", flow.Status)
	}
	deadline := time.After(time.Second)
	for {
		current, err := service.Flow(flow.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != flowPending {
			if current.Status != flowSuccess || current.VerificationURL != "https://example.test/connect" {
				t.Fatalf("first connection did not publish its URL: %+v", current)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("first connection did not finish")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestFailedStatusCheckDoesNotCreateAnotherApp(t *testing.T) {
	for name, output := range map[string]string{
		"network":            "network offline",
		"credentials":        `{"ok":false,"error":{"type":"authorization","subtype":"invalid_credentials"}}`,
		"missing profile":    `{"ok":false,"error":{"type":"config","subtype":"not_configured","field":"--profile"}}`,
		"malformed response": `{"error":`,
	} {
		t.Run(name, func(t *testing.T) {
			service := &Service{runner: commandFunc(func(context.Context, string, []string, string) ([]byte, error) {
				return []byte(output), errors.New("status check failed")
			})}
			if _, err := service.BeginLarkSetup(t.Context()); err == nil {
				t.Fatal("created app after failed status check")
			}
		})
	}
}

func newFinalizeTestService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	base, err := os.ReadFile("../../conf/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, base, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.RuntimeOverridePath(path), []byte("extract:\n  enabled: false\n  principal_open_id: ou_desktop_onboarding_pending\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile("../../conf/prompts/cc-system-prompt.md")
	if err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(root, "conf", "prompts", "cc-system-prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, prompt, 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.PrincipalProfile{}, &domain.Task{}); err != nil {
		t.Fatal(err)
	}
	runner := commandFunc(func(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
		if strings.Join(args, " ") == "login status" {
			return []byte("Logged in"), nil
		}
		if args[0] == "event" {
			return []byte(`{"ok":true,"data":{"decision":{"status":"ready"}}}`), nil
		}
		if args[0] == "config" {
			t.Fatal("Finalize must not configure a second lark app")
		}
		return (onboardingRunnerStub{}).Run(ctx, bin, args, input)
	})
	service, err := NewService(Options{
		Desktop: true, ConfigPath: path, RuntimeRoot: root, StateRoot: root, DB: db,
		LarkCLIBin: "lark-cli", AgentCLIBin: "traex", TaskSubmitter: &taskcreate.Submitter{},
		HTTPClient: credentialClient(t, `{"code":0,"tenant_access_token":"test-token"}`), Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.runtimeID = "before-restart"
	return service
}

func TestFreshDesktopIdentityAndOneAppFinalize(t *testing.T) {
	service := newFinalizeTestService(t)
	name, principal, err := service.savedIdentity()
	if err != nil || name != "" || principal != "" {
		t.Fatal("bootstrap placeholder treated as real principal")
	}
	result, err := service.Finalize(t.Context(), "", "test@example.com", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if result.AgentName != "Jarvis" || !result.Configuration.MachineConfigurationReady || result.RuntimeID != "before-restart" {
		t.Fatalf("unexpected final state: %#v", result)
	}
	if result.AppReady || result.WorldModelReady || result.Completed {
		t.Fatalf("saved configuration must not mark the old runtime ready: %#v", result)
	}
	saved, err := service.savedSecret("cli_test")
	if err != nil || saved != "test-secret" {
		t.Fatal("same-app channel credential missing")
	}
	name, principal, err = service.savedIdentity()
	if err != nil || name != "Jarvis" || principal != "ou_principal" {
		t.Fatal("identity was not inferred from lark-cli")
	}
	if _, err := os.Stat(filepath.Join(service.options.StateRoot, "restart.requested")); err != nil {
		t.Fatal("restart request must be written before Finalize succeeds")
	}
	restarted, err := NewService(service.options)
	if err != nil {
		t.Fatal(err)
	}
	status, err := restarted.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !status.AppReady || status.Completed || status.RuntimeID == service.runtimeID {
		t.Fatalf("new runtime should be usable while world modeling is pending: %+v", status)
	}
}

func TestFailedFinalizeRemainsNotReadyAndCanRetry(t *testing.T) {
	for _, marker := range []string{worldModelMarkerName, "restart.requested"} {
		t.Run(marker, func(t *testing.T) {
			service := newFinalizeTestService(t)
			blocked := filepath.Join(service.options.StateRoot, marker)
			if err := os.Mkdir(blocked, 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Finalize(t.Context(), "", "test@example.com", "test-secret"); err == nil {
				t.Fatal("Finalize ignored a marker write failure")
			}
			status, err := service.Status(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !status.Configuration.MachineConfigurationReady || status.AppReady {
				t.Fatalf("partial save must not make this process ready: %+v", status)
			}
			if err := os.Remove(blocked); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Finalize(t.Context(), "", "test@example.com", ""); err != nil {
				t.Fatalf("retry should reuse saved credentials: %v", err)
			}
		})
	}
}

func TestConnectionRetryReturnsPendingSetupFlow(t *testing.T) {
	service := &Service{runner: unconfiguredSetupRunner{}, flows: map[string]*Flow{
		"pending": {ID: "pending", Status: flowPending, kind: "lark_setup", VerificationURL: "https://example.test/connect"},
	}}
	flow, err := service.BeginLarkSetup(t.Context())
	if err != nil || flow.ID != "pending" || flow.VerificationURL != "https://example.test/connect" {
		t.Fatalf("retry did not resume existing connection: %+v, %v", flow, err)
	}
	if len(service.flows) != 1 {
		t.Fatal("retry created another flow")
	}
	if _, err := service.CancelFlow(flow.ID); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionRetryDoesNotReuseOtherLogin(t *testing.T) {
	service := &Service{runner: unconfiguredSetupRunner{}, flows: map[string]*Flow{
		"agent": {ID: "agent", Status: flowPending},
	}}
	if _, err := service.BeginLarkSetup(t.Context()); err == nil {
		t.Fatal("connection must not return an unrelated login flow")
	}
}

func TestBotEventsAreOnlyProbedAndFailuresAreNotReady(t *testing.T) {
	for _, response := range []string{`{}`, `{"ok":true,"data":{"decision":{"status":"blocked"}}}`, `{"ok":false,"data":{"decision":{"status":"ready"}}}`} {
		service := &Service{runner: commandFunc(func(_ context.Context, _ string, args []string, _ string) ([]byte, error) {
			if strings.Join(args[len(args)-3:], " ") != "--as bot --dry-run" {
				t.Fatal("must not open a second websocket")
			}
			return []byte(response), nil
		})}
		if err := service.checkBotEvents(t.Context()); err == nil {
			t.Fatal("unready event passed")
		}
	}
}

func (r unconfiguredSetupRunner) RunJSON(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
	return r.Run(ctx, bin, args, input)
}
