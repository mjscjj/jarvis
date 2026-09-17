package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEventChecksPreserveAllFailuresAndDistinguishMalformedOutput(t *testing.T) {
	s := &Service{options: Options{Desktop: true}, runner: commandFunc(func(_ context.Context, _ string, args []string, _ string) ([]byte, error) {
		if args[2] == "im.message.receive_v1" {
			return []byte(`{"ok":true,"data":{"decision":{"status":"blocked","preconditions":[{"name":"console_event_published","status":"missing"}]}}}`), nil
		}
		return []byte("broken JSON from old CLI"), nil
	})}
	checks := s.botEventChecks(t.Context())
	if len(checks) != 2 || checks[0].Ready || checks[1].Ready {
		t.Fatalf("missing check results: %+v", checks)
	}
	if !strings.Contains(checks[0].Error, "console_event_published") || !strings.Contains(checks[1].Error, "无法解析") || !strings.Contains(checks[1].Error, "broken JSON") {
		t.Fatalf("diagnostics lost or misclassified: %+v", checks)
	}
	if err := botEventsError(checks); err == nil || !strings.Contains(err.Error(), "im.message.receive_v1") || !strings.Contains(err.Error(), "card.action.trigger") {
		t.Fatalf("did not report both failures: %v", err)
	}
}

func TestEventChecksParseStdoutWithoutHumanDiagnostics(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "lark-cli")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 'using current application' >&2\necho '{\"ok\":true,\"data\":{\"decision\":{\"status\":\"ready\"}}}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Service{options: Options{Desktop: true, LarkCLIBin: binary}, runner: execRunner{}}
	if err := s.checkBotEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestStatusExposesMissingEventBeforeFinalize(t *testing.T) {
	s := newFinalizeTestService(t)
	original := s.runner
	s.runner = commandFunc(func(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
		if len(args) > 2 && args[0] == "event" && args[2] == "card.action.trigger" {
			return []byte(`{"ok":true,"data":{"decision":{"status":"blocked","reason":"event not published"}}}`), nil
		}
		return original.RunJSON(ctx, bin, args, input)
	})
	status, err := s.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Lark.Bot.Verified || !status.Lark.User.Verified || status.AppReady || len(status.Lark.ApplicationChecks) != 2 || status.Lark.ApplicationChecks[1].Ready {
		t.Fatalf("application failure conflated with login or hidden: %+v", status)
	}
	if _, err := s.Finalize(t.Context(), "", "test@example.com", "test-secret"); err == nil || !strings.Contains(err.Error(), "event not published") {
		t.Fatalf("Finalize ignored event failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.options.StateRoot, "cc-connect", "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote chat configuration before the application passed checks")
	}
}

func TestPermissionExportUsesExactlyTheOAuthRequestScopes(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "lark-cli")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' \"$4\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Service{options: Options{RuntimeRoot: root, LarkCLIBin: binary}, runner: execRunner{}}
	exported, err := s.PermissionConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Scopes struct{ Tenant, User []string }
	}
	if err := json.Unmarshal(exported, &config); err != nil {
		t.Fatal(err)
	}
	requested, err := s.larkAuthorization(t.Context(), "begin")
	if err != nil || !reflect.DeepEqual(config.Scopes.User, strings.Fields(string(requested))) {
		t.Fatalf("export differs from OAuth request: %v", err)
	}
	for _, required := range []string{"im:message:readonly", "im:message:send_as_bot", "docs:document.comment:create"} {
		if !strings.Contains(" "+strings.Join(config.Scopes.Tenant, " ")+" ", " "+required+" ") {
			t.Errorf("missing Bot capability: %s", required)
		}
	}
	for _, scope := range config.Scopes.Tenant {
		if strings.HasPrefix(scope, "minutes:") {
			t.Fatalf("copied user permissions into Bot permissions: %s", scope)
		}
	}
}
