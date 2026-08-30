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
		case strings.Contains(command, "--begin"):
			return []byte(`{"event":"qr_image_ready","data":{"complete_token":"resume-1","verification_uri_complete":"https://sso.example/login","user_code":"ABCD"}}`), nil
		case strings.Contains(command, "--complete resume-1"):
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
}

func TestCompleteKeepsPendingFlow(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case command == "--json auth status":
			return []byte(`{"status":"success","data":{"authenticated":false}}`), nil
		case strings.Contains(command, "--begin"):
			return []byte(`{"data":{"complete_token":"resume-1","verification_url":"https://sso.example/login"}}`), nil
		case strings.Contains(command, "--complete"):
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
