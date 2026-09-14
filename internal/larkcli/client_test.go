package larkcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type testResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Value     string `json:"value"`
		ItemError string `json:"item_error"`
	} `json:"data"`
}

const fixtureCommandTimeout = 30 * time.Second

func TestRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}

	tests := []struct {
		name          string
		script        string
		timeout       time.Duration
		wantValue     string
		wantItemError string
		wantErr       string
		wantAPIErr    bool
		wantCmdErr    bool
	}{
		{
			name:      "success",
			script:    `printf '%s' '{"ok":true,"data":{"value":"captured"}}'`,
			wantValue: "captured",
		},
		{
			name:       "api error with zero exit",
			script:     `printf '%s' '{"ok":false,"error":{"type":"api","subtype":"rate_limited","message":"slow down"}}'`,
			wantErr:    "slow down",
			wantAPIErr: true,
		},
		{
			name:          "partial data with zero exit",
			script:        `printf '%s' '{"ok":false,"data":{"item_error":"No read permission"}}'`,
			wantErr:       "ok=false without error",
			wantItemError: "No read permission",
		},
		{
			name:    "invalid json",
			script:  `printf '%s' 'not-json'`,
			wantErr: "decode lark-cli envelope",
		},
		{
			name:       "non-zero exit",
			script:     `printf '%s' 'boom' >&2; exit 7`,
			wantErr:    "boom",
			wantCmdErr: true,
		},
		{
			name:       "structured api error on stderr",
			script:     `printf '%s\n' '[page 1] fetching...' >&2; printf '%s' '{"ok":false,"error":{"type":"authorization","subtype":"missing_scope","code":99991679,"message":"login required","missing_scopes":["im:chat:read"]}}' >&2; exit 1`,
			wantErr:    "missing_scope",
			wantAPIErr: true,
			wantCmdErr: true,
		},
		{
			name:          "non-zero exit preserves structured stdout",
			script:        `printf '%s' '{"ok":false,"data":{"item_error":"No read permission"}}'; printf '%s' 'batch failed' >&2; exit 1`,
			wantErr:       "batch failed",
			wantItemError: "No read permission",
			wantCmdErr:    true,
		},
		{
			name:       "timeout",
			script:     `sleep 1`,
			timeout:    20 * time.Millisecond,
			wantErr:    "deadline exceeded",
			wantCmdErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := writeScript(t, tt.script)
			timeout := tt.timeout
			if timeout == 0 {
				timeout = fixtureCommandTimeout
			}
			client, err := New(testOptions(bin, timeout))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			var got testResponse
			err = client.Run(context.Background(), &got, "im", "+chat-list")
			if got.Data.ItemError != tt.wantItemError {
				t.Fatalf("Run() item_error = %q, want %q", got.Data.ItemError, tt.wantItemError)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Run() error = %v", err)
				}
				if got.Data.Value != tt.wantValue {
					t.Fatalf("Run() value = %q, want %q", got.Data.Value, tt.wantValue)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run() error = %v, want containing %q", err, tt.wantErr)
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) != tt.wantAPIErr {
				t.Errorf("errors.As(APIError) = %v, want %v", errors.As(err, &apiErr), tt.wantAPIErr)
			}
			var cmdErr *CommandError
			if errors.As(err, &cmdErr) != tt.wantCmdErr {
				t.Errorf("errors.As(CommandError) = %v, want %v", errors.As(err, &cmdErr), tt.wantCmdErr)
			}
		})
	}
}

func TestSearchUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}

	t.Run("parses candidates and has_more", func(t *testing.T) {
		body := `printf '%s' '{"ok":true,"data":{"users":[{"open_id":"ou_abc","localized_name":"测试用户","email":"c@x.com","department":"公会","p2p_chat_id":"oc_1","is_cross_tenant":false,"has_chatted":true}],"has_more":true}}'`
		client, err := New(testOptions(writeScript(t, body), fixtureCommandTimeout))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		users, hasMore, err := client.SearchUser(context.Background(), "测试用户")
		if err != nil {
			t.Fatalf("SearchUser() error = %v", err)
		}
		if !hasMore {
			t.Fatalf("SearchUser() has_more = false, want true")
		}
		if len(users) != 1 || users[0].OpenID != "ou_abc" || users[0].LocalizedName != "测试用户" || users[0].P2PChatID != "oc_1" {
			t.Fatalf("SearchUser() users = %+v, unexpected", users)
		}
	})

	t.Run("rejects empty query without calling CLI", func(t *testing.T) {
		client, err := New(testOptions(writeScript(t, `exit 1`), fixtureCommandTimeout))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if _, _, err := client.SearchUser(context.Background(), "  "); err == nil || !strings.Contains(err.Error(), "query is empty") {
			t.Fatalf("SearchUser() error = %v, want query is empty", err)
		}
	})
}

// auth status has no {ok:...} envelope, and a ready-looking user block is the
// only thing that proves every `--as user` call will still work.
func TestVerifyUserIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}

	const ready = `{"identity":"user","verified":true,"identities":{"bot":{"status":"ready"},"user":{"status":"ready","available":true,"verified":true,"tokenStatus":"valid","userName":"储节节","openId":"ou_principal"}}}`
	const cleared = `{"identity":"bot","identities":{"bot":{"status":"ready"},"user":{"status":"missing","available":false,"message":"User identity: missing (no token in keychain for ou_principal)","openId":"ou_principal"}}}`
	const expired = `{"identity":"bot","identities":{"user":{"status":"missing","available":false,"tokenStatus":"expired","message":"User identity: missing (refresh token expired)"}}}`
	// A token that is present but rejected upstream: --verify is what catches it.
	const unverified = `{"identity":"user","identities":{"user":{"status":"ready","available":true,"verified":false,"tokenStatus":"invalid"}}}`

	for _, test := range []struct {
		name    string
		script  string
		wantErr string
	}{
		{name: "ready", script: `printf '%s' ` + shellQuote(ready)},
		{name: "token cleared", script: `printf '%s' ` + shellQuote(cleared), wantErr: `status="missing"`},
		{name: "refresh token expired", script: `printf '%s' ` + shellQuote(expired), wantErr: `token="expired"`},
		{name: "present but not verified", script: `printf '%s' ` + shellQuote(unverified), wantErr: "verified=false"},
		{name: "cli failure", script: `printf '%s' 'keychain locked' >&2; exit 3`, wantErr: "keychain locked"},
		{name: "invalid json", script: `printf '%s' 'not-json'`, wantErr: "decode lark-cli auth status"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := New(Options{Bin: writeScript(t, test.script), RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout, Timezone: "Asia/Shanghai"})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			user, err := client.VerifyUserIdentity(context.Background())
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("VerifyUserIdentity() error = %v, want it to mention %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("VerifyUserIdentity() error = %v", err)
			}
			if user.UserName != "储节节" || user.OpenID != "ou_principal" || user.TokenStatus != "valid" {
				t.Fatalf("VerifyUserIdentity() user = %+v, unexpected", user)
			}
		})
	}
}

// The real `auth status` prints JSON natively and rejects --format, so the argv
// this builds must not carry the flag every other shortcut gets.
func TestVerifyUserIdentityAsksForVerificationWithoutFormatFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	bin := writeScript(t, `
case "$*" in
  "auth status --verify") printf '%s' '{"identities":{"user":{"status":"ready","available":true,"verified":true,"tokenStatus":"valid","userName":"储节节","openId":"ou_principal"}}}' ;;
  *) printf '%s' "unexpected args: $*" >&2; exit 9 ;;
esac`)
	client, err := New(Options{Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.VerifyUserIdentity(context.Background()); err != nil {
		t.Fatalf("VerifyUserIdentity() error = %v", err)
	}
}

func TestListChatMembersUsesUnlimitedPaginationAndRejectsTruncation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}

	t.Run("returns complete user roster", func(t *testing.T) {
		bin := writeScript(t, `
case " $* " in
  *" --page-all --page-limit 0 "*) printf '%s' '{"ok":true,"data":{"users":[{"member_id":"ou_1","name":"Alice","tenant_key":"t1"}],"user_total":1,"truncations":[]}}' ;;
  *) printf '%s' "missing unlimited pagination: $*" >&2; exit 9 ;;
esac`)
		client, err := New(testOptions(bin, fixtureCommandTimeout))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		members, err := client.ListChatMembers(context.Background(), "oc_1")
		if err != nil {
			t.Fatalf("ListChatMembers() error = %v", err)
		}
		if len(members) != 1 || members[0].MemberID != "ou_1" {
			t.Fatalf("ListChatMembers() = %#v", members)
		}
	})

	t.Run("fails on server-side truncation", func(t *testing.T) {
		body := `printf '%s' '{"ok":true,"data":{"users":[],"user_total":150,"truncations":[{"limit":100,"member_type":"user"}]}}'`
		client, err := New(testOptions(writeScript(t, body), fixtureCommandTimeout))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if _, err := client.ListChatMembers(context.Background(), "oc_large"); err == nil || !strings.Contains(err.Error(), "incomplete user roster") {
			t.Fatalf("ListChatMembers() error = %v, want incomplete user roster", err)
		}
	})

	t.Run("fails when pagination remains", func(t *testing.T) {
		body := `printf '%s' '{"ok":true,"data":{"users":[],"user_total":150,"has_more":true,"page_token":"next","truncations":[]}}'`
		client, err := New(testOptions(writeScript(t, body), fixtureCommandTimeout))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if _, err := client.ListChatMembers(context.Background(), "oc_large"); err == nil || !strings.Contains(err.Error(), "has_more=true") {
			t.Fatalf("ListChatMembers() error = %v, want has_more incompleteness", err)
		}
	})
}

func TestRunRejectsCallerFormat(t *testing.T) {
	client := &Client{}
	err := client.Run(context.Background(), &testResponse{}, "im", "+chat-list", "--format", "pretty")
	if err == nil || !strings.Contains(err.Error(), "owned by the client") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunUsesDefaultProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	bin := writeScript(t, `
case "$*" in
  "im +chat-list --format json") printf '%s' '{"ok":true,"data":{"value":"default-profile"}}' ;;
  *) printf '%s' "unexpected args: $*" >&2; exit 9 ;;
esac`)
	client, err := New(testOptions(bin, fixtureCommandTimeout))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var got testResponse
	if err := client.Run(context.Background(), &got, "im", "+chat-list"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got.Data.Value != "default-profile" {
		t.Fatalf("Run() value = %q, want default-profile", got.Data.Value)
	}
}

func TestCreateMarkdownDocumentIsolatesConcurrentUsers(t *testing.T) {
	for _, key := range []string{"LARKSUITE_CLI_APP_ID", "LARKSUITE_CLI_APP_SECRET", "LARKSUITE_CLI_USER_ACCESS_TOKEN", "LARKSUITE_CLI_TENANT_ACCESS_TOKEN", "LARKSUITE_CLI_PROFILE", "LARKSUITE_CLI_AUTH_PROXY", "LARKSUITE_CLI_PROXY_KEY"} {
		t.Setenv(key, "wrong-inherited-identity")
	}
	bin := writeScript(t, `
[ "$*" = 'docs +create --title Weekly --doc-format markdown --content - --as user --format json' ] || exit 9
[ "$(cat)" = '# Progress' ] || exit 8
[ -z "$LARKSUITE_CLI_APP_SECRET$LARKSUITE_CLI_TENANT_ACCESS_TOKEN$LARKSUITE_CLI_PROFILE$LARKSUITE_CLI_AUTH_PROXY$LARKSUITE_CLI_PROXY_KEY" ] || exit 7
case "$LARKSUITE_CLI_APP_ID:$LARKSUITE_CLI_USER_ACCESS_TOKEN" in
  cli_login:user-a) doc=a ;;
  cli_login:user-b) doc=b ;;
  *) exit 6 ;;
esac
printf '{"ok":true,"data":{"document":{"document_id":"doc-%s","url":"https://example.test/docx/created"},"warnings":["conversion warning"]}}' "$doc"
`)
	opts := testOptions(bin, fixtureCommandTimeout)
	opts.Concurrency = 2
	client, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, token := range []string{"user-a", "user-b"} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			doc, err := client.CreateMarkdownDocument(context.Background(), UserCredentials{AppID: "cli_login", AccessToken: token}, " Weekly ", " # Progress ")
			if err != nil {
				t.Errorf("export failed: %v", err)
				return
			}
			// Each concurrent caller must receive its own document.
			if doc.DocumentID != "doc-"+strings.TrimPrefix(token, "user-") || len(doc.Warnings) != 1 {
				t.Errorf("unexpected document: %+v", doc)
			}
		}(token)
	}
	wg.Wait()
	if os.Getenv("LARKSUITE_CLI_USER_ACCESS_TOKEN") != "wrong-inherited-identity" {
		t.Fatal("server environment was modified")
	}
}

func TestCreateMarkdownDocumentRejectsMissingCredentials(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "called")
	t.Setenv("EXPORT_CALL_MARKER", marker)
	client, err := New(testOptions(writeScript(t, `touch "$EXPORT_CALL_MARKER"; exit 1`), fixtureCommandTimeout))
	if err != nil {
		t.Fatal(err)
	}
	for _, credentials := range []UserCredentials{{}, {AppID: "cli_login"}, {AccessToken: "user-a"}} {
		if _, err := client.CreateMarkdownDocument(context.Background(), credentials, "Weekly", "# Progress"); err == nil {
			t.Fatal("missing credentials accepted")
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("CLI called without explicit credentials")
	}
}

func TestCreateMarkdownDocumentRedactsCredentialsInErrors(t *testing.T) {
	for _, status := range []string{"0", "3"} {
		t.Run(status, func(t *testing.T) {
			client, err := New(testOptions(writeScript(t, `printf '{"ok":false,"error":{"type":"authorization","subtype":"missing_scope","message":"echo %s"}}' "$LARKSUITE_CLI_USER_ACCESS_TOKEN" | tee /dev/stderr
exit `+status), fixtureCommandTimeout))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CreateMarkdownDocument(context.Background(), UserCredentials{AppID: "cli_login", AccessToken: "super-secret-user-token"}, "Weekly", "# Progress")
			var apiErr *APIError
			if err == nil || strings.Contains(err.Error(), "super-secret-user-token") || !errors.As(err, &apiErr) || apiErr.Subtype != "missing_scope" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRunUsesConfiguredTimezoneInsteadOfHostTimezone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	t.Setenv("TZ", "UTC")
	bin := writeScript(t, `printf '{"ok":true,"data":{"value":"%s"}}' "$TZ"`)
	client, err := New(testOptions(bin, fixtureCommandTimeout))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var got testResponse
	if err := client.Run(context.Background(), &got, "im", "+chat-list"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got.Data.Value != "Asia/Shanghai" {
		t.Fatalf("Run() TZ = %q, want Asia/Shanghai", got.Data.Value)
	}
}

func TestNewRejectsMissingTimezone(t *testing.T) {
	opts := testOptions(writeScript(t, `exit 0`), fixtureCommandTimeout)
	opts.Timezone = ""
	if _, err := New(opts); err == nil || !strings.Contains(err.Error(), "timezone is empty") {
		t.Fatalf("New() error = %v, want timezone is empty", err)
	}
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-lark-cli")
	content := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write fake lark-cli: %v", err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func testOptions(bin string, timeout time.Duration) Options {
	return Options{
		Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: timeout,
		Timezone: "Asia/Shanghai",
	}
}
