package larkcli

import (
	"runtime"
	"strings"
	"testing"
)

func TestBroadcastSenderUsesExplicitAppAndEmailWithoutUserCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	script := `
case "$*" in
  "profile list")
    printf '%s' '[{"name":"main","appId":"cli_a96a0c8d82b85cb1"},{"name":"notify","appId":"cli_a96a2422f03bdbd7"}]'
    ;;
  "--profile main auth status --verify")
    printf '%s' '{"appId":"cli_a96a0c8d82b85cb1","verified":true,"identities":{"bot":{"status":"ready","available":true,"verified":true,"appName":"Jarvis Bot"},"user":{"status":"ready","available":true,"verified":true,"tokenStatus":"valid"}}}'
    ;;
  "--profile notify auth status --verify")
    printf '%s' '{"appId":"cli_a96a2422f03bdbd7","verified":true,"identities":{"bot":{"status":"ready","available":true,"verified":true,"appName":"Jarvis通知机器人"}}}'
    ;;
  "--profile notify api GET /open-apis/application/v6/scopes --as bot --format json")
    printf '%s' '{"ok":true,"data":{"scopes":[{"grant_status":1,"scope_name":"im:message:send_as_bot","scope_type":"tenant"},{"grant_status":1,"scope_name":"im:message:send_multi_users","scope_type":"tenant"}]}}'
    ;;
  "--profile notify api GET /open-apis/application/v2/app/visibility --params "*" --as bot --format json")
    printf '%s' '{"ok":true,"data":{"is_visible_to_all":1}}'
    ;;
  "--profile notify api POST /open-apis/im/v1/messages --params "*" --data "*" --as bot --format json")
    case "$*" in
      *'"content":"{\"schema\":\"2.0\"}"'*'"msg_type":"interactive"'*'"receive_id":"recipient@example.com"'*'"uuid":"comment-key"'*) ;;
      *) printf '%s' "unexpected send data: $*" >&2; exit 8 ;;
    esac
    printf '%s' '{"ok":true,"data":{"message_id":"om_notice"}}'
    ;;
  "--profile notify im +messages-mget --message-ids om_notice --as bot --format json")
    printf '%s' '{"ok":true,"data":{"messages":[{"message_id":"om_notice"}],"total":1}}'
    ;;
  *)
    printf '%s' "unexpected args: $*" >&2
    exit 9
    ;;
esac
`
	client, err := New(testOptions(writeScript(t, script), fixtureCommandTimeout))
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewBroadcastSender(t.Context(), client, "cli_a96a2422f03bdbd7", "notify")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sender.SendCardToEmail(t.Context(), "recipient@example.com", "author@example.com", `{"schema":"2.0"}`, "comment-key"); err != nil {
		t.Fatal(err)
	}
}

func TestBroadcastSenderRejectsWrongPreferredProfileWithoutFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	script := `printf '%s' '[{"name":"main","appId":"cli_a96a0c8d82b85cb1"},{"name":"wrong","appId":"cli_wrong"}]'`
	client, err := New(testOptions(writeScript(t, script), fixtureCommandTimeout))
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewBroadcastSender(t.Context(), client, "cli_a96a2422f03bdbd7", "wrong")
	if err == nil || !strings.Contains(err.Error(), "uses app") {
		t.Fatalf("error = %v, want wrong app rejection", err)
	}
}

func TestBroadcastSenderSkipsAuthorByEmail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	script := `
case "$*" in
  "profile list") printf '%s' '[{"name":"main","appId":"cli_a96a0c8d82b85cb1"},{"name":"notify","appId":"cli_a96a2422f03bdbd7"}]' ;;
  "--profile main auth status --verify") printf '%s' '{"appId":"cli_a96a0c8d82b85cb1","verified":true,"identities":{"bot":{"status":"ready","available":true,"verified":true,"appName":"Jarvis Bot"},"user":{"status":"ready","available":true,"verified":true,"tokenStatus":"valid"}}}' ;;
  "--profile notify auth status --verify") printf '%s' '{"appId":"cli_a96a2422f03bdbd7","verified":true,"identities":{"bot":{"status":"ready","available":true,"verified":true,"appName":"Jarvis通知机器人"}}}' ;;
  "--profile notify api GET /open-apis/application/v6/scopes --as bot --format json") printf '%s' '{"ok":true,"data":{"scopes":[{"grant_status":1,"scope_name":"im:message:send_as_bot","scope_type":"tenant"},{"grant_status":1,"scope_name":"im:message:send_multi_users","scope_type":"tenant"}]}}' ;;
  "--profile notify api GET /open-apis/application/v2/app/visibility --params "*" --as bot --format json") printf '%s' '{"ok":true}' ;;
  "--profile main contact +search-user --user-ids ou_self --as user --format json") printf '%s' '{"ok":true,"data":{"users":[{"open_id":"ou_self","localized_name":"作者","enterprise_email":"author@example.com"}]}}' ;;
  *) printf '%s' "unexpected send: $*" >&2; exit 9 ;;
esac
`
	client, err := New(testOptions(writeScript(t, script), fixtureCommandTimeout))
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewBroadcastSender(t.Context(), client, "cli_a96a2422f03bdbd7", "notify")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sender.SendCardToEmail(t.Context(), "author@example.com", "author@example.com", `{"schema":"2.0"}`, "comment-key"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCard2RejectsInvalidPayload(t *testing.T) {
	for _, value := range []string{"", "not json", `{"schema":"1.0"}`} {
		if err := validateCard2(value); err == nil {
			t.Fatalf("validateCard2(%q) succeeded, want error", value)
		}
	}
}
