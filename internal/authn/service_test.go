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

func TestLoginCompletesDeviceFlow(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(command, "auth login --begin --session --session-method qr"):
			return []byte(`{"event":"qr_image_ready","data":{"complete_token":"resume-1","verification_uri_complete":"https://sso.example/login","user_code":"ABCD"}}`), nil
		case strings.HasSuffix(command, "auth login --complete resume-1"):
			return []byte(`{"status":"success","data":{"login_mode":"session","login_status":"success"}}`), nil
		case strings.HasSuffix(command, "auth userinfo"):
			return []byte(`{"status":"success","data":{"username":"alice","email":"alice@bytedance.com"}}`), nil
		case strings.HasSuffix(command, "auth clear --yes"):
			return []byte(`{"status":"success"}`), nil
		default:
			return nil, errors.New("unexpected command: " + command)
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

func TestCompleteRejectsIdentityOutsideAllowList(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(command, "auth login --begin --session --session-method qr"):
			return []byte(`{"data":{"complete_token":"resume-1","verification_url":"https://sso.example/login"}}`), nil
		case strings.HasSuffix(command, "auth login --complete resume-1"):
			return []byte(`{"status":"success","data":{"login_mode":"session","login_status":"success"}}`), nil
		case strings.HasSuffix(command, "auth userinfo"):
			return []byte(`{"data":{"username":"mallory","email":"mallory@bytedance.com"}}`), nil
		case strings.HasSuffix(command, "auth clear --yes"):
			return []byte(`{"status":"success"}`), nil
		default:
			return nil, errors.New("unexpected command: " + command)
		}
	}})
	begin, err := service.Login(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(t.Context(), *begin.FlowID); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("Complete() error = %v, want ErrNotAllowed", err)
	}
}

func TestCompleteKeepsPendingFlow(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(command, "auth login --begin --session --session-method qr"):
			return []byte(`{"data":{"complete_token":"resume-1","verification_url":"https://sso.example/login"}}`), nil
		case strings.HasSuffix(command, "auth login --complete resume-1"):
			return []byte(`{"status":"success","data":{"login_mode":"session","login_status":"pending","complete_token":"resume-1"}}`), nil
		default:
			return nil, errors.New("unexpected command: " + command)
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
	service := newTestService(t, fakeRunner{run: func(_ string, _ []string) ([]byte, error) { return nil, nil }})
	result, err := service.startSession(User{Username: "alice", Email: "alice@bytedance.com", IsPrincipal: true})
	if err != nil {
		t.Fatal(err)
	}
	service.Logout(result.SessionToken)
	if _, ok := service.Authenticate(result.SessionToken); ok {
		t.Fatal("session remained authenticated after logout")
	}
}

func TestDisabledAuthenticationDoesNotInvokeByteDanceSSO(t *testing.T) {
	service, err := NewServiceWithRunner("bytedcli", 12*time.Hour, false, nil, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		t.Fatal("disabled authentication must not invoke bytedcli")
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if view := service.Status(""); view.Enabled || view.Status != StatusUnauthenticated || view.User != nil {
		t.Fatalf("Status() = %#v, want disabled view", view)
	}
	result, err := service.Login(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Enabled || result.Status != StatusUnauthenticated || result.SessionToken != "" {
		t.Fatalf("Login() = %#v, want disabled view without session", result)
	}
}

func newTestService(t *testing.T, runner CommandRunner) *Service {
	t.Helper()
	service, err := NewServiceWithRunner("bytedcli", 12*time.Hour, true, []string{"alice", "alice@bytedance.com"}, runner)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestSessionLoginLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		pending      bool
	}{
		{"waiting for scan", "pending", true},
		{"expired QR", "expired", false},
		{"unknown result", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleared := false
			var profile string
			service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
				if len(args) < 5 || args[0] != "--json" || args[1] != "--profile" || !strings.HasPrefix(args[2], "jarvis-web-") {
					t.Fatalf("unisolated command: %v", args)
				}
				if profile == "" {
					profile = args[2]
				} else if profile != args[2] {
					t.Fatalf("profile changed: %v", args)
				}
				command := strings.Join(args[3:], " ")
				switch command {
				case "auth login --begin --session --session-method qr":
					return []byte(`{"status":"success","data":{"login_mode":"session","login_status":"pending","complete_token":"resume-1","verification_uri_complete":"https://sso.bytedance.com/qr"}}`), nil
				case "auth login --complete resume-1":
					return []byte(`{"status":"success","data":{"login_mode":"session","login_status":"` + tc.status + `"}}`), nil
				case "auth clear --yes":
					cleared = true
					return []byte(`{"status":"success"}`), nil
				default:
					t.Fatalf("must not look up identity before success: %s", command)
					return nil, nil
				}
			}})
			begin, err := service.Login(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Complete(t.Context(), *begin.FlowID)
			if err == nil || result.SessionToken != "" {
				t.Fatalf("unfinished login issued session: %+v, %v", result, err)
			}
			if errors.Is(err, ErrPending) != tc.pending {
				t.Fatalf("pending mismatch: %v", err)
			}
			_, remains := service.pendingFlow(*begin.FlowID)
			if remains != tc.pending || cleared == tc.pending {
				t.Fatalf("flow remains=%v cleared=%v", remains, cleared)
			}
		})
	}
}

func TestSessionIdentityRequiresActualUserInfo(t *testing.T) {
	for _, raw := range []string{
		`{"data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`,
		`{"data":{"username":"alice"}}`,
	} {
		service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
			if strings.Join(args[3:], " ") != "auth userinfo" {
				t.Fatalf("args=%v", args)
			}
			return []byte(raw), nil
		}})
		if _, err := service.probe(t.Context(), "jarvis-web-test"); err == nil {
			t.Fatal("accepted SDK identity or incomplete session identity")
		}
	}
}
