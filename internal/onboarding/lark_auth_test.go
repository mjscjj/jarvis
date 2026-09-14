package onboarding

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInstallAuthorizationRequestsAndChecksSameCapabilities(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// Capture argv, including a binary path with spaces, without touching OAuth.
	binary := filepath.Join(t.TempDir(), "fake lark-cli")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Service{options: Options{RuntimeRoot: root, LarkCLIBin: binary}, runner: execRunner{}}
	var requests [][]string
	for _, action := range []string{"begin", "check"} {
		out, err := s.larkAuthorization(t.Context(), action)
		if err != nil {
			t.Fatalf("%s: %s, %v", action, out, err)
		}
		requests = append(requests, strings.Split(strings.TrimSpace(string(out)), "\n"))
	}
	begin, check := requests[0], requests[1]
	if len(begin) != 6 || !reflect.DeepEqual(begin[:3], []string{"auth", "login", "--scope"}) ||
		!reflect.DeepEqual(begin[4:], []string{"--no-wait", "--json"}) {
		t.Fatalf("begin must start a nonblocking scoped login: %q", begin)
	}
	if !reflect.DeepEqual(check, []string{"auth", "check", "--scope", begin[3], "--json"}) {
		t.Fatalf("check differs from requested scopes: %q", check)
	}
	scopes := strings.Fields(begin[3])
	seen := map[string]bool{}
	for _, scope := range scopes {
		if seen[scope] {
			t.Fatalf("duplicate scope: %s", scope)
		}
		seen[scope] = true
		if strings.HasPrefix(scope, "okr:") || strings.HasPrefix(scope, "mail:") || strings.HasPrefix(scope, "approval:") ||
			strings.HasPrefix(scope, "contact:user.department") || scope == "contact:user.employee:readonly" {
			t.Fatalf("unneeded or unsupported permission requested: %s", scope)
		}
	}
	for _, scope := range []string{
		"im:message:readonly", "search:message", "search:docs:read", "docs:document.content:read",
		"calendar:calendar.event:read", "vc:meeting.search:read", "vc:record:readonly", "vc:note:read",
		"minutes:minutes.basic:read", "minutes:minutes.artifacts:read", "minutes:minutes.search:read",
		"task:task:read", "sheets:spreadsheet:read", "base:record:read", "wiki:node:read",
	} {
		if !seen[scope] {
			t.Errorf("built-in capability missing: %s", scope)
		}
	}
}

func TestExistingLoginCannotSkipMissingInstallationScopes(t *testing.T) {
	for _, checkErr := range []error{nil, errors.New("exit 1")} {
		s := &Service{options: Options{RuntimeRoot: "/runtime", LarkCLIBin: "lark-cli"}}
		checked := false
		s.runner = commandFunc(func(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
			if bin == "bash" {
				if !reflect.DeepEqual(args, []string{"/runtime/scripts/jarvis-lark-auth", "check", "lark-cli"}) {
					t.Fatalf("unexpected auth invocation: %q", args)
				}
				checked = true
				return []byte(`{"missing_scopes":["minutes:minutes.artifacts:read"]}`), checkErr
			}
			return (onboardingRunnerStub{}).Run(ctx, bin, args, input)
		})
		status := s.larkStatus(t.Context())
		if !checked || !status.Bot.Verified || status.User.Verified != (checkErr == nil) {
			t.Fatalf("existing token bypassed scope check: %+v", status)
		}
		if checkErr != nil && !strings.Contains(status.Error, "minutes:minutes.artifacts:read") {
			t.Fatalf("missing scope evidence lost: %s", status.Error)
		}
	}
}

func TestLarkLoginUsesSharedEntryAndCompletesSameDeviceFlow(t *testing.T) {
	completed := make(chan struct{})
	s := &Service{options: Options{RuntimeRoot: "/runtime", LarkCLIBin: "lark-cli"}, flows: make(map[string]*Flow)}
	s.runner = commandFunc(func(_ context.Context, bin string, args []string, _ string) ([]byte, error) {
		if bin == "bash" && reflect.DeepEqual(args, []string{"/runtime/scripts/jarvis-lark-auth", "begin", "lark-cli"}) {
			return []byte(`{"device_code":"this-device","verification_uri":"https://example.test/short","verification_url":"https://example.test/verify","verification_uri_complete":"https://example.test/authorize"}`), nil
		}
		if bin != "lark-cli" || !reflect.DeepEqual(args, []string{"auth", "login", "--device-code", "this-device"}) {
			return nil, errors.New("unexpected device completion command")
		}
		close(completed)
		return []byte(`{"ok":true}`), nil
	})
	flow, err := s.BeginLarkLogin(t.Context())
	if err != nil || flow.VerificationURL != "https://example.test/authorize" {
		t.Fatalf("begin: %v, %v", flow, err)
	}

	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("device flow was not completed with the same code")
	}
}

func TestFindStringSkipsNonStringFieldBeforeNestedValue(t *testing.T) {
	value := decodeJSON([]byte(`{"device_code":{"legacy":true},"data":{"device_code":"nested-device"}}`))
	if got := findString(value, "device_code"); got != "nested-device" {
		t.Fatalf("findString() = %q, want nested-device", got)
	}
}

func TestLarkLoginRefreshCancelsPreviousPendingLogin(t *testing.T) {
	var beginCalls int
	service := &Service{options: Options{RuntimeRoot: "/runtime", LarkCLIBin: "lark-cli"}, flows: map[string]*Flow{
		"old": {ID: "old", Status: flowPending, kind: "lark_login", VerificationURL: "https://example.test/old"},
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if !service.registerFlowCancel("old", cancel) {
		t.Fatal("cancel registration failed")
	}
	service.runner = commandFunc(func(_ context.Context, bin string, args []string, _ string) ([]byte, error) {
		if bin == "bash" && reflect.DeepEqual(args, []string{"/runtime/scripts/jarvis-lark-auth", "begin", "lark-cli"}) {
			beginCalls++
			return []byte(`{"device_code":"new-device","verification_url":"https://example.test/new"}`), nil
		}
		if bin == "lark-cli" && reflect.DeepEqual(args, []string{"auth", "login", "--device-code", "new-device"}) {
			return []byte(`{"ok":true}`), nil
		}
		return nil, errors.New("unexpected command")
	})
	flow, err := service.BeginLarkLogin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if flow.Status != flowPending || flow.VerificationURL != "https://example.test/new" || flow.ID == "old" {
		t.Fatalf("new flow = %+v", flow)
	}
	if beginCalls != 1 {
		t.Fatalf("begin calls = %d", beginCalls)
	}
	if ctx.Err() == nil {
		t.Fatal("previous pending login was not cancelled")
	}
	old, err := service.Flow("old")
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != flowFailed || !strings.Contains(old.Error, "重新生成") {
		t.Fatalf("old flow = %+v", old)
	}
}

func TestInstallAuthorizationFailsWhenRuntimeScriptIsMissing(t *testing.T) {
	s := &Service{options: Options{RuntimeRoot: t.TempDir(), LarkCLIBin: "lark-cli"}, runner: execRunner{}}
	if _, err := s.larkAuthorization(t.Context(), "begin"); err == nil {
		t.Fatal("missing runtime script silently accepted")
	}
	cmd := exec.CommandContext(t.Context(), "bash", "../../scripts/jarvis-lark-auth", "invalid")
	if err := cmd.Run(); err == nil {
		t.Fatal("invalid action accepted")
	}
}
