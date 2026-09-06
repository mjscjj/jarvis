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

func testOptions(bin string, timeout time.Duration) Options {
	return Options{
		Bin: bin, RateLimit: 100, Burst: 1, Concurrency: 1, Timeout: timeout,
		Timezone: "Asia/Shanghai",
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
