package larkcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestUserDirectorySharedCacheDoesNotWaitForAvatars(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	script := `echo "$*" >> '` + log + `'
case "$*" in
 *"contact +search-user"*) sleep 0.1; printf '%s' '{"ok":true,"data":{"has_more":true,"users":[{"open_id":"ou_a","localized_name":"甲","enterprise_email":"a@example.test"}]}}' ;;
 *"/search/v1/user"*) printf '%s' '{"ok":true,"data":{"users":[{"open_id":"ou_a","avatar":{"avatar_240":"https://example.test/avatar"}}]}}' ;;
 *) exit 9 ;;
esac`
	client, _ := New(testOptions(writeScript(t, script), fixtureCommandTimeout))
	d := &Directory{client: client, profile: "fixed", appID: "cli_fixed", identity: "user", cacheFile: filepath.Join(t.TempDir(), "cache")}
	d.initUserCache("ou_self")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, more, err := d.SearchPage(t.Context(), "甲")
			if err != nil || !more || len(p) != 1 {
				t.Errorf("page=%v more=%v err=%v", p, more, err)
			}
		}()
	}
	wg.Wait()
	if _, err := d.Resolve(t.Context(), "a@example.test"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(log)
	if strings.Count(string(raw), "contact +search-user") != 1 || strings.Contains(string(raw), "/search/v1/user") {
		t.Fatalf("duplicate or blocking calls: %s", raw)
	}
	if _, err := d.Avatar(t.Context(), "unknown@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Avatar(t.Context(), "a@example.test"); err != nil {
		t.Fatal(err)
	}
	next := &Directory{appID: d.appID, identity: "user", cacheFile: d.cacheFile}
	next.initUserCache("ou_self")
	p, _, err := next.SearchPage(context.Background(), "甲")
	if err != nil || p[0].AvatarURL == "" {
		t.Fatalf("restart cache=%v %v", p, err)
	}
	other := &Directory{appID: d.appID, identity: "user", cacheFile: d.cacheFile}
	other.initUserCache("ou_other")
	if _, ok := other.cachedPage("甲"); ok {
		t.Fatal("cache leaked across directory identities")
	}
}
