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
)

func TestConnectingExistingLarkAppNeverCreatesAnother(t *testing.T) {
	service := &Service{runner: commandFunc(func(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
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

func TestFailedStatusCheckDoesNotCreateAnotherApp(t *testing.T) {
	service := &Service{runner: commandFunc(func(context.Context, string, []string, string) ([]byte, error) {
		return nil, errors.New("network offline")
	})}
	if _, err := service.BeginLarkSetup(t.Context()); err == nil {
		t.Fatal("created app after failed status check")
	}
}

func TestFreshDesktopIdentityAndOneAppFinalize(t *testing.T) {
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
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.PrincipalProfile{}, &domain.Task{}); err != nil {
		t.Fatal(err)
	}
	service := &Service{options: Options{ConfigPath: path, RuntimeRoot: root, StateRoot: root, DB: db, HTTPClient: credentialClient(t, `{"code":0,"tenant_access_token":"test-token"}`)}, runtimeID: "before-restart"}
	service.runner = commandFunc(func(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
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
	saved, err := service.savedSecret("cli_test")
	if err != nil || saved != "test-secret" {
		t.Fatal("same-app channel credential missing")
	}
	name, principal, err = service.savedIdentity()
	if err != nil || name != "Jarvis" || principal != "ou_principal" {
		t.Fatal("identity was not inferred from lark-cli")
	}
	// Wait for the marker callback so it cannot outlive this temporary fixture.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, "restart.requested")); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("restart request was not written")
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
