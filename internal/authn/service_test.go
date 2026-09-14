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
		case strings.HasSuffix(command, "auth login --begin"):
			return []byte(`{"event":"qr_image_ready","data":{"complete_token":"resume-1","verification_uri_complete":"https://sso.example/login","user_code":"ABCD"}}`), nil
		case strings.HasSuffix(command, "auth login --complete resume-1"):
			return []byte(`{"status":"success","data":{"status":"success","authStatus":{"authenticated":true,"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
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
		case strings.HasSuffix(command, "auth login --begin"):
			return []byte(`{"data":{"complete_token":"resume-1","verification_url":"https://sso.example/login"}}`), nil
		case strings.HasSuffix(command, "auth login --complete resume-1"):
			return []byte(`{"status":"success","data":{"status":"success","authStatus":{"authenticated":true,"identity":{"username":"mallory","email":"mallory@bytedance.com"}}}}`), nil
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
		case strings.HasSuffix(command, "auth login --begin"):
			return []byte(`{"data":{"complete_token":"resume-1","verification_url":"https://sso.example/login"}}`), nil
		case strings.HasSuffix(command, "auth login --complete resume-1"):
			return []byte(`{"status":"error","error":{"code":"AUTHORIZATION_PENDING","message":"pending"}}`), errors.New("exit 1")
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

func TestCompletionKeepsFlowUntilVerifiedIdentityArrives(t *testing.T) {
	for _, first := range []struct {
		name string
		raw  string
		err  error
	}{
		{"successful_pending_response", `{"status":"success","data":{"status":"pending","mode":"init"}}`, nil},
		{"poll_timeout", "", context.DeadlineExceeded},
	} {
		t.Run(first.name, func(t *testing.T) {
			polls, clears := 0, 0
			service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
				command := strings.Join(args, " ")
				switch {
				case strings.HasSuffix(command, "auth login --begin"):
					return []byte(`{"data":{"complete_token":"resume-1","verification_url":"https://sso.example/login"}}`), nil
				case strings.HasSuffix(command, "auth login --complete resume-1"):
					polls++
					if polls == 1 {
						return []byte(first.raw), first.err
					}
					return []byte(`{"status":"success","data":{"status":"success","authStatus":{"authenticated":true,"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
				case strings.HasSuffix(command, "auth clear --yes"):
					clears++
					return []byte(`{"status":"success"}`), nil
				default:
					t.Fatalf("login must not query unrelated auth status: %v", args)
					return nil, nil
				}
			}})
			begin, err := service.Login(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Complete(t.Context(), *begin.FlowID); !errors.Is(err, ErrPending) {
				t.Fatalf("first poll: %v", err)
			}
			if clears != 0 {
				t.Fatal("pending profile was cleared")
			}
			result, err := service.Complete(t.Context(), *begin.FlowID)
			if err != nil {
				t.Fatal(err)
			}
			if user, ok := service.Authenticate(result.SessionToken); !ok || user.Username != "alice" || clears != 1 {
				t.Fatalf("completed flow: user=%#v authenticated=%v clears=%d", user, ok, clears)
			}
		})
	}
}

func TestCompletionNeverIssuesSessionWithoutVerifiedIdentity(t *testing.T) {
	for _, result := range []string{
		`{"status":"success","data":{"status":"pending","authStatus":{"authenticated":true,"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`,
		`{"status":"success","data":{"status":"success","authStatus":{"authenticated":false,"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`,
		`{"status":"success","data":{"status":"success"}}`,
		`{"status":"success","data":{"status":"denied"}}`,
	} {
		service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
			if strings.HasSuffix(strings.Join(args, " "), "auth login --begin") {
				return []byte(`{"data":{"complete_token":"resume-1","verification_url":"https://sso.example/login"}}`), nil
			}
			return []byte(result), nil
		}})
		begin, err := service.Login(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		login, err := service.Complete(t.Context(), *begin.FlowID)
		if err == nil || login.SessionToken != "" {
			t.Fatalf("unverified completion accepted: %s", result)
		}
	}
}
