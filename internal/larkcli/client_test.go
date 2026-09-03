package larkcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
			client, err := New(Options{Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: timeout})
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
		client, err := New(Options{Bin: writeScript(t, body), RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout})
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
		client, err := New(Options{Bin: writeScript(t, `exit 1`), RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout})
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
			client, err := New(Options{Bin: writeScript(t, test.script), RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout})
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
	client, err := New(Options{Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.VerifyUserIdentity(context.Background()); err != nil {
		t.Fatalf("VerifyUserIdentity() error = %v", err)
	}
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
	client, err := New(Options{Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout})
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

func TestCreateMarkdownDocumentUsesApplicationIdentityAndStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	bin := writeScript(t, `
if [ "$*" = 'docs +create --title Weekly --doc-format markdown --content - --as bot --format json' ]; then
  input=$(cat)
  if [ "$input" != "# Progress" ]; then
    printf '%s' "unexpected stdin: $input" >&2
    exit 8
  fi
  printf '%s' '{"ok":true,"data":{"document":{"document_id":"docx_1","url":"https://example.test/docx_1"},"warnings":["one warning"]}}'
  exit 0
fi
if [ "$*" = 'drive +secure-label-list --as user --format json' ]; then
  printf '%s' '{"ok":true,"data":{"items":[{"id":"7439268224199852036","name":"L1-Public"},{"id":"7439288234140483587","name":"L2-Internal"}]}}'
  exit 0
fi
if [ "$*" = 'drive +secure-label-update --token docx_1 --type docx --label-id 7439288234140483587 --as user --format json' ]; then
  printf '%s' '{"ok":true,"data":{}}'
  exit 0
fi
if [ "$*" = 'drive permission.members auth --params {"token":"docx_1","type":"docx","action":"manage_public"} --as bot --format json' ]; then
  printf '%s' '{"ok":true,"data":{"auth_result":true}}'
  exit 0
fi
if [ "$*" = 'drive permission.public patch --params {"token":"docx_1","type":"docx"} --data {"link_share_entity":"tenant_editable"} --as bot --yes --format json' ]; then
  printf '%s' '{"ok":true,"data":{"permission_public":{"link_share_entity":"tenant_editable"}}}'
  exit 0
fi
if [ "$*" = 'drive permission.public get --params {"token":"docx_1","type":"docx"} --as bot --format json' ]; then
  printf '%s' '{"ok":true,"data":{"permission_public":{"link_share_entity":"tenant_editable"}}}'
  exit 0
fi
printf '%s' "unexpected args: $*" >&2
exit 9`)
	client, err := New(Options{Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout, ExportSecureLabel: "L2-Internal"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	document, err := client.CreateMarkdownDocument(context.Background(), " Weekly ", " # Progress ")
	if err != nil {
		t.Fatalf("CreateMarkdownDocument() error = %v", err)
	}
	if document.DocumentID != "docx_1" || document.URL != "https://example.test/docx_1" || len(document.Warnings) != 1 || document.LinkShareEntity != "tenant_editable" {
		t.Fatalf("CreateMarkdownDocument() = %+v", document)
	}
}

func TestCreateMarkdownDocumentFailsWhenManagePublicIsUnauthorized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	bin := writeScript(t, `
if [ "$*" = 'docs +create --title Weekly --doc-format markdown --content - --as bot --format json' ]; then
  printf '%s' '{"ok":true,"data":{"document":{"document_id":"docx_1","url":"https://example.test/docx_1"}}}'
  exit 0
fi
if [ "$*" = 'drive +secure-label-list --as user --format json' ]; then
  printf '%s' '{"ok":true,"data":{"items":[{"id":"7439268224199852036","name":"L1-Public"},{"id":"7439288234140483587","name":"L2-Internal"}]}}'
  exit 0
fi
if [ "$*" = 'drive +secure-label-update --token docx_1 --type docx --label-id 7439288234140483587 --as user --format json' ]; then
  printf '%s' '{"ok":true,"data":{}}'
  exit 0
fi
if [ "$*" = 'drive permission.members auth --params {"token":"docx_1","type":"docx","action":"manage_public"} --as bot --format json' ]; then
  printf '%s' '{"ok":true,"data":{"auth_result":false}}'
  exit 0
fi
printf '%s' "unexpected args: $*" >&2
exit 9`)
	client, err := New(Options{Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout, ExportSecureLabel: "L2-Internal"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = client.CreateMarkdownDocument(context.Background(), "Weekly", "# Progress")
	if err == nil || !strings.Contains(err.Error(), "not authorized") || !strings.Contains(err.Error(), "https://example.test/docx_1") {
		t.Fatalf("CreateMarkdownDocument() error = %v", err)
	}
}

func TestCreateMarkdownDocumentFailsWhenPermissionReadBackDoesNotMatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	bin := writeScript(t, `
if [ "$*" = 'docs +create --title Weekly --doc-format markdown --content - --as bot --format json' ]; then
  printf '%s' '{"ok":true,"data":{"document":{"document_id":"docx_1","url":"https://example.test/docx_1"}}}'
  exit 0
fi
if [ "$*" = 'drive +secure-label-list --as user --format json' ]; then
  printf '%s' '{"ok":true,"data":{"items":[{"id":"7439268224199852036","name":"L1-Public"},{"id":"7439288234140483587","name":"L2-Internal"}]}}'
  exit 0
fi
if [ "$*" = 'drive +secure-label-update --token docx_1 --type docx --label-id 7439288234140483587 --as user --format json' ]; then
  printf '%s' '{"ok":true,"data":{}}'
  exit 0
fi
if [ "$*" = 'drive permission.members auth --params {"token":"docx_1","type":"docx","action":"manage_public"} --as bot --format json' ]; then
  printf '%s' '{"ok":true,"data":{"auth_result":true}}'
  exit 0
fi
if [ "$*" = 'drive permission.public patch --params {"token":"docx_1","type":"docx"} --data {"link_share_entity":"tenant_editable"} --as bot --yes --format json' ]; then
  printf '%s' '{"ok":true,"data":{"permission_public":{"link_share_entity":"tenant_editable"}}}'
  exit 0
fi
if [ "$*" = 'drive permission.public get --params {"token":"docx_1","type":"docx"} --as bot --format json' ]; then
  printf '%s' '{"ok":true,"data":{"permission_public":{"link_share_entity":"tenant_readable"}}}'
  exit 0
fi
printf '%s' "unexpected args: $*" >&2
exit 9`)
	client, err := New(Options{Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout, ExportSecureLabel: "L2-Internal"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = client.CreateMarkdownDocument(context.Background(), "Weekly", "# Progress")
	if err == nil || !strings.Contains(err.Error(), `got link_share_entity="tenant_readable"`) || !strings.Contains(err.Error(), "https://example.test/docx_1") {
		t.Fatalf("CreateMarkdownDocument() error = %v", err)
	}
}

func TestCreateMarkdownDocumentFailsWhenSecureLabelIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	bin := writeScript(t, `
if [ "$*" = 'docs +create --title Weekly --doc-format markdown --content - --as bot --format json' ]; then
  printf '%s' '{"ok":true,"data":{"document":{"document_id":"docx_1","url":"https://example.test/docx_1"}}}'
  exit 0
fi
if [ "$*" = 'drive +secure-label-list --as user --format json' ]; then
  printf '%s' '{"ok":true,"data":{"items":[{"id":"7439268224199852036","name":"L1-Public"}]}}'
  exit 0
fi
printf '%s' "unexpected args: $*" >&2
exit 9`)
	client, err := New(Options{Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: fixtureCommandTimeout, ExportSecureLabel: "L2-Internal"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = client.CreateMarkdownDocument(context.Background(), "Weekly", "# Progress")
	if err == nil || !strings.Contains(err.Error(), `secure label "L2-Internal" is not available`) || !strings.Contains(err.Error(), "L1-Public") {
		t.Fatalf("CreateMarkdownDocument() error = %v", err)
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
