package larkcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirectoryBotIndexesAllPagesAndRejectsAppLocalIDs(t *testing.T) {
	script := `
case "$*" in
 *"--profile notify api GET /open-apis/contact/v3/scopes"*) printf '%s' '{"ok":true,"data":{"user_ids":["on_a"],"department_ids":["od_root"]}}' ;;
 *"/departments/od_root/children"*) printf '%s' '{"ok":true,"data":{"items":[]}}' ;;
 *"/users/find_by_department"*"next"*) printf '%s' '{"ok":true,"data":{"items":[{"name":"乙","email":"b@example.test","union_id":"on_b"}]}}' ;;
 *"/users/find_by_department"*) printf '%s' '{"ok":true,"data":{"items":[{"name":"甲","email":"a@EXAMPLE.TEST","union_id":"on_a"}],"has_more":true,"page_token":"next"}}' ;;
 *"/users/batch"*) printf '%s' '{"ok":true,"data":{"items":[{"name":"甲","email":"a@example.test","union_id":"on_a"}]}}' ;;
 *) exit 9 ;;
esac`
	client, err := New(testOptions(writeScript(t, script), fixtureCommandTimeout))
	if err != nil {
		t.Fatal(err)
	}
	d := &Directory{client: client, profile: "notify", appID: "cli_notify", identity: "bot", cacheFile: filepath.Join(t.TempDir(), "cache.json")}
	got, err := d.Search(t.Context(), "example.test")
	if err != nil || len(got) != 2 {
		t.Fatalf("people=%+v err=%v", got, err)
	}
	if got[0].Email != "a@example.test" || got[0].UnionID != "on_a" {
		t.Fatalf("person=%+v", got[0])
	}
	// A warm cache must be independent of CLI availability.
	d.client = nil
	if _, err = d.Resolve(t.Context(), "b@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Resolve(t.Context(), "ou_a"); err == nil {
		t.Fatal("app-local ID accepted as email")
	}
	raw, err := os.ReadFile(d.cacheFile)
	if err != nil || !strings.Contains(string(raw), "cli_notify") {
		t.Fatal("cache not app scoped", err)
	}
}

func TestDirectoryFailedRefreshPreservesPreviousSnapshot(t *testing.T) {
	client, _ := New(testOptions(writeScript(t, `printf '%s' '{"ok":true,"data":{"has_more":true,"page_token":"repeat"}}'`), fixtureCommandTimeout))
	before := time.Now().Add(-time.Hour)
	d := &Directory{client: client, profile: "notify", identity: "bot", people: []DirectoryPerson{{Email: "old@example.test"}}, refreshed: before}
	_, err := d.Search(t.Context(), "old")
	if err == nil || !strings.Contains(err.Error(), "pagination") {
		t.Fatalf("err=%v", err)
	}
	if len(d.people) != 1 || !d.refreshed.Equal(before) {
		t.Fatal("partial refresh replaced good snapshot")
	}
}

func TestExplicitProfileIgnoresAmbientIdentityEnvironment(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_APP_ID", "wrong-app")
	t.Setenv("LARKSUITE_CLI_USER_ACCESS_TOKEN", "wrong-user")
	client, _ := New(testOptions(writeScript(t, `[ -z "$LARKSUITE_CLI_APP_ID" ] && [ -z "$LARKSUITE_CLI_USER_ACCESS_TOKEN" ] || exit 8
printf '%s' '{"ok":true}'`), fixtureCommandTimeout))
	var out any
	if err := client.Run(t.Context(), &out, "--profile", "notify", "auth", "status"); err != nil {
		t.Fatal(err)
	}
}
