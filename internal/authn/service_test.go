package authn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	run func(string, []string) ([]byte, error)
}

func (f fakeRunner) Run(_ context.Context, bin string, args ...string) ([]byte, error) {
	return f.run(bin, args)
}

func TestLoginUsesExistingByteDanceIdentity(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(bin string, args []string) ([]byte, error) {
		if bin != "bytedcli" || strings.Join(args, " ") != "--json auth status" {
			t.Fatalf("command = %s %s", bin, strings.Join(args, " "))
		}
		return []byte(`{"status":"success","data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
	}})

	result, err := service.Login(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusAuthenticated || result.User == nil || result.User.Username != "alice" {
		t.Fatalf("result = %#v", result)
	}
	if !result.User.IsPrincipal {
		t.Fatal("server identity was not marked as principal")
	}
	if _, ok := service.Authenticate(result.SessionToken); !ok {
		t.Fatal("new session is not authenticated")
	}
}

func TestLoginCompletesDeviceFlow(t *testing.T) {
	statusCalls := 0
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case command == "--json auth status":
			statusCalls++
			if statusCalls == 1 {
				return []byte(`{"status":"success","data":{"authenticated":false}}`), nil
			}
			return []byte(`{"status":"success","data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
		case command == "--json auth login --begin":
			return []byte(`{"event":"qr_image_ready","data":{"complete_token":"resume-1","verification_uri_complete":"https://sso.example/login","user_code":"ABCD"}}`), nil
		case command == "--json auth login --complete resume-1":
			return []byte(`{"status":"success","data":{"authenticated":true}}`), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}})

	begin, err := service.Login(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if begin.Status != StatusPending || begin.FlowID == nil || begin.VerificationURL == nil {
		t.Fatalf("begin = %#v", begin)
	}
	complete, err := service.Complete(t.Context(), *begin.FlowID)
	if err != nil {
		t.Fatal(err)
	}
	if complete.Status != StatusAuthenticated || complete.SessionToken == "" {
		t.Fatalf("complete = %#v", complete)
	}
	if complete.User == nil || !complete.User.IsPrincipal {
		t.Fatalf("completed local user = %#v", complete.User)
	}
}

func TestIsolatedLoginUsesOwnProfileAndClassifiesOtherUser(t *testing.T) {
	var profile string
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "--profile" {
			if profile == "" {
				profile = args[1]
			} else if args[1] != profile {
				t.Fatalf("profile changed: %q -> %q", profile, args[1])
			}
			switch strings.Join(args[2:], " ") {
			case "--json auth login --begin":
				return []byte(`{"data":{"complete_token":"resume-remote","verification_url":"https://sso.example/login"}}`), nil
			case "--json auth login --complete resume-remote":
				return []byte(`{"status":"success","data":{"authenticated":true}}`), nil
			case "--json auth status":
				return []byte(`{"data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"bob","email":"bob@bytedance.com"}}}}`), nil
			case "--json auth logout":
				return []byte(`{"status":"success"}`), nil
			}
		}
		if strings.Join(args, " ") == "--json auth status" {
			return []byte(`{"data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
		}
		return nil, errors.New("unexpected command: " + strings.Join(args, " "))
	}})

	begin, err := service.LoginIsolated(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if begin.FlowID == nil || profile != isolatedLoginProfile {
		t.Fatalf("begin/profile = %#v / %q", begin, profile)
	}
	complete, err := service.Complete(t.Context(), *begin.FlowID)
	if err != nil {
		t.Fatal(err)
	}
	if complete.User == nil || complete.User.Username != "bob" || complete.User.IsPrincipal {
		t.Fatalf("completed isolated user = %#v", complete.User)
	}
}

func TestIsolatedLoginAllowsOnlyOnePendingFlowUntilExpiry(t *testing.T) {
	beginCalls := 0
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--profile jarvis-web --json auth logout":
			return []byte(`{"status":"success"}`), nil
		case "--profile jarvis-web --json auth login --begin":
			beginCalls++
			return []byte(`{"data":{"complete_token":"resume-remote","verification_url":"https://sso.example/login"}}`), nil
		default:
			return nil, errors.New("unexpected command: " + strings.Join(args, " "))
		}
	}})
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if _, err := service.LoginIsolated(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.LoginIsolated(t.Context()); !errors.Is(err, ErrLoginInProgress) {
		t.Fatalf("second LoginIsolated() error = %v, want ErrLoginInProgress", err)
	}
	if beginCalls != 1 {
		t.Fatalf("begin calls = %d, want 1", beginCalls)
	}

	now = now.Add(loginFlowTTL)
	if _, err := service.LoginIsolated(t.Context()); err != nil {
		t.Fatalf("LoginIsolated() after expiry error = %v", err)
	}
	if beginCalls != 2 {
		t.Fatalf("begin calls after expiry = %d, want 2", beginCalls)
	}
}

func TestIsolatedLoginTerminalFailureReleasesPendingFlow(t *testing.T) {
	completeCalls := 0
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--profile jarvis-web --json auth logout":
			return []byte(`{"status":"success"}`), nil
		case "--profile jarvis-web --json auth login --begin":
			return []byte(`{"data":{"complete_token":"resume-remote","verification_url":"https://sso.example/login"}}`), nil
		case "--profile jarvis-web --json auth login --complete resume-remote":
			completeCalls++
			return []byte(`{"status":"error","error":{"code":"ACCESS_DENIED","message":"denied"}}`), errors.New("exit 1")
		default:
			return nil, errors.New("unexpected command: " + strings.Join(args, " "))
		}
	}})

	begin, err := service.LoginIsolated(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(t.Context(), *begin.FlowID); err == nil {
		t.Fatal("Complete() error = nil, want terminal authorization error")
	}
	if completeCalls != 1 {
		t.Fatalf("complete calls = %d, want 1", completeCalls)
	}
	if _, err := service.LoginIsolated(t.Context()); err != nil {
		t.Fatalf("LoginIsolated() after terminal failure error = %v", err)
	}
}

func TestCompleteKeepsPendingFlow(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case command == "--json auth status":
			return []byte(`{"status":"success","data":{"authenticated":false}}`), nil
		case command == "--json auth login --begin":
			return []byte(`{"data":{"complete_token":"resume-1","verification_url":"https://sso.example/login"}}`), nil
		case command == "--json auth login --complete resume-1":
			return []byte(`{"status":"error","error":{"code":"AUTHORIZATION_PENDING","message":"pending"}}`), errors.New("exit 1")
		default:
			return nil, errors.New("unexpected command")
		}
	}})

	begin, err := service.Login(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(t.Context(), *begin.FlowID); !errors.Is(err, ErrPending) {
		t.Fatalf("Complete() error = %v, want ErrPending", err)
	}
}

func TestLogoutInvalidatesOnlyJarvisSession(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return []byte(`{"data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
	}})
	result, err := service.Login(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	service.Logout(result.SessionToken)
	if _, ok := service.Authenticate(result.SessionToken); ok {
		t.Fatal("session remained authenticated after logout")
	}
}

func newTestService(t *testing.T, runner CommandRunner) *Service {
	t.Helper()
	service, err := NewServiceWithRunner("bytedcli", 12*time.Hour, runner)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
