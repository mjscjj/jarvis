package authn

import (
	"context"
	"errors"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	run func(string, []string) ([]byte, error)
}

func TestBrowserSSOEndpointOnlyOverridesChildEnvironment(t *testing.T) {
	t.Setenv("BYTECLOUD_CLI_API_BASE_URL", "https://host-tools.example")
	runner := execRunner{apiBaseURL: "https://cloud.byteintl.net"}
	output, err := runner.Run(t.Context(), "sh", "-c", `printf '%s' "$BYTECLOUD_CLI_API_BASE_URL"`)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "https://cloud.byteintl.net" {
		t.Fatalf("SSO subprocess endpoint = %q", output)
	}
	if got := os.Getenv("BYTECLOUD_CLI_API_BASE_URL"); got != "https://host-tools.example" {
		t.Fatalf("SSO changed the host tools environment: %q", got)
	}
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
	service, err := NewServiceWithRunner(openAuthTestDB(t), "bytedcli", 12*time.Hour, false, nil, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
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
	service, err := NewServiceWithRunner(openAuthTestDB(t), "bytedcli", 12*time.Hour, true, []string{"alice", "alice@bytedance.com"}, runner)
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

func TestCompletionAutomaticallyRenewsLostOrExpiredFlow(t *testing.T) {
	for _, reason := range []string{"restart", "ttl", "expired", "invalid_ticket", "cli_expired"} {
		t.Run(reason, func(t *testing.T) {
			begins := 0
			service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
				command := strings.Join(args, " ")
				switch {
				case strings.HasSuffix(command, "auth login --begin"):
					begins++
					return []byte(`{"data":{"complete_token":"resume","verification_url":"https://sso.example/login"}}`), nil
				case strings.Contains(command, "auth login --complete"):
					if reason == "cli_expired" {
						return []byte(`{"error":{"code":"BYTECLOUD_AUTH_LOGIN_EXPIRED"}}`), errors.New("exit 1")
					}
					return []byte(`{"data":{"status":"` + reason + `"}}`), nil
				case strings.HasSuffix(command, "auth clear --yes"):
					return []byte(`{}`), nil
				default:
					t.Fatalf("unexpected command: %s", command)
					return nil, nil
				}
			}})
			begin, err := service.Login(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			switch reason {
			case "restart":
				service.flows = make(map[string]flow)
			case "ttl":
				service.now = func() time.Time { return time.Now().Add(loginFlowTTL + time.Minute) }
			}
			renewed, err := service.Complete(t.Context(), *begin.FlowID)
			if err != nil {
				t.Fatal(err)
			}
			if renewed.Status != StatusPending || renewed.FlowID == nil || *renewed.FlowID == *begin.FlowID || begins != 2 || renewed.SessionToken != "" {
				t.Fatalf("flow was not renewed: %#v; begins=%d", renewed, begins)
			}
		})
	}
}

func TestCompletionRetriesTransportFailureWithoutReplacingFlow(t *testing.T) {
	polls, begins := 0, 0
	service := newTestService(t, fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(command, "auth login --begin"):
			begins++
			return []byte(`{"data":{"complete_token":"resume","verification_url":"https://sso.example/login"}}`), nil
		case strings.Contains(command, "auth login --complete"):
			polls++
			if polls == 1 {
				return nil, errors.New("connection reset")
			}
			return []byte(`{"data":{"status":"success","authStatus":{"authenticated":true,"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
		case strings.HasSuffix(command, "auth clear --yes"):
			return []byte(`{}`), nil
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}})
	begin, err := service.Login(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(t.Context(), *begin.FlowID); err == nil {
		t.Fatal("expected transport error")
	}
	result, err := service.Complete(t.Context(), *begin.FlowID)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionToken == "" || begins != 1 {
		t.Fatalf("original authorization was lost: %#v", result)
	}
}

func TestLoginReportsFailureInsteadOfProgressHint(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return []byte("{\"event\":\"action_required\",\"data\":{\"message\":\"Retry with --debug\"}}\n" +
			`{"status":"error","error":{"code":"BYTECLOUD_AUTH_RUNTIME_ERROR","message":"net/http: TLS handshake timeout"}}`), errors.New("exit 1")
	}})
	_, err := service.Login(t.Context())
	if err == nil || !strings.Contains(err.Error(), "TLS handshake timeout") || strings.Contains(err.Error(), "--debug") {
		t.Fatalf("expected underlying login failure, got %v", err)
	}
}

func openAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
