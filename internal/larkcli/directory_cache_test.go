package larkcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestUserDirectoryAvatarWarmsColdIdentityOnce(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	script := `echo "$*" >> '` + log + `'
case "$*" in
 *"contact +search-user"*) printf '%s' '{"ok":true,"data":{"users":[{"open_id":"ou_cold","localized_name":"冷启动","enterprise_email":"cold@example.test"}]}}' ;;
 *"/search/v1/user"*) printf '%s' '{"ok":true,"data":{"users":[{"open_id":"ou_cold","name":"冷启动","avatar":{"avatar_240":"https://example.test/cold"}}]}}' ;;
 *) exit 9 ;;
esac`
	client, _ := New(testOptions(writeScript(t, script), fixtureCommandTimeout))
	d := &Directory{client: client, profile: "fixed", appID: "cli_fixed", identity: "user", cacheFile: filepath.Join(t.TempDir(), "cache")}
	d.initUserCache("ou_self")

	first, err := d.Avatar(t.Context(), "cold@example.test")
	if err != nil || first.AvatarURL == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := d.Avatar(t.Context(), "cold@example.test")
	if err != nil || second.AvatarURL != first.AvatarURL {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	raw, _ := os.ReadFile(log)
	if strings.Count(string(raw), "contact +search-user") != 1 || strings.Count(string(raw), "/search/v1/user") != 1 {
		t.Fatalf("cold avatar calls were not cached: %s", raw)
	}
}

func TestUserDirectoryCacheTTLAndNormalizedKeys(t *testing.T) {
	now := time.Now()
	d := &Directory{userCache: directoryCache{
		Queries: map[string]directoryPage{
			"alice": {People: []DirectoryPerson{{Email: "alice@example.test"}}, At: now.Add(-299 * time.Minute)},
			"old":   {People: []DirectoryPerson{{Email: "old@example.test"}}, At: now.Add(-301 * time.Minute)},
			"empty": {People: []DirectoryPerson{}, At: now.Add(-time.Minute)},
			"blank": {People: []DirectoryPerson{}, At: now.Add(-3 * time.Minute)},
		},
		People: map[string]cachedDirectoryPerson{
			"alice@example.test": {Person: DirectoryPerson{Email: "alice@example.test"}, At: now.Add(-99 * time.Hour)},
			"old@example.test":   {Person: DirectoryPerson{Email: "old@example.test"}, At: now.Add(-101 * time.Hour)},
		},
	}}

	if _, ok := d.cachedPage("  ALICE "); !ok {
		t.Fatal("positive query should remain cached for 300 minutes and use a normalized key")
	}
	if _, ok := d.cachedPage("old"); ok {
		t.Fatal("positive query older than 300 minutes should expire")
	}
	if _, ok := d.cachedPage("empty"); !ok {
		t.Fatal("empty query should remain cached for 2 minutes")
	}
	if _, ok := d.cachedPage("blank"); ok {
		t.Fatal("empty query older than 2 minutes should expire")
	}

	d.saveUserCache()
	if _, ok := d.userCache.People["alice@example.test"]; !ok {
		t.Fatal("identity should remain cached for 100 hours")
	}
	if _, ok := d.userCache.People["old@example.test"]; ok {
		t.Fatal("identity older than 100 hours should expire")
	}
}
