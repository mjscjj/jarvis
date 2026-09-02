package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStoreSavesOneFilePerPerson(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := NewTokenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	alice := Grant{
		User:             User{OpenID: "ou_alice", Name: "Alice", Email: "alice@example.com"},
		AccessToken:      "u-alice",
		RefreshToken:     "r-alice",
		ExpiresIn:        2 * time.Hour,
		RefreshExpiresIn: 1440 * time.Hour,
	}
	bob := Grant{User: User{OpenID: "ou_bob", Name: "Bob"}, AccessToken: "u-bob", RefreshToken: "r-bob"}
	if _, err := store.Save(alice, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(bob, now); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("token files = %d, want 2", len(entries))
	}

	raw, err := os.ReadFile(filepath.Join(dir, "ou_alice.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk["name"] != "Alice" || onDisk["access_token"] != "u-alice" || onDisk["refresh_token"] != "r-alice" {
		t.Fatalf("on-disk record = %v", onDisk)
	}

	stored, found, err := store.Load("ou_bob")
	if err != nil || !found {
		t.Fatalf("Load(ou_bob) found=%t, err=%v", found, err)
	}
	if stored.AccessToken != "u-bob" || stored.Name != "Bob" {
		t.Fatalf("stored = %+v", stored)
	}
	// Feishu omits the lifetimes for some grants; no expiry must be recorded
	// rather than one that already elapsed.
	if stored.ExpiresAt != nil || stored.RefreshExpiresAt != nil {
		t.Fatalf("unexpected expiry: %v / %v", stored.ExpiresAt, stored.RefreshExpiresAt)
	}
}

func TestTokenStoreOverwritesOnSecondLogin(t *testing.T) {
	t.Parallel()
	store, err := NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	first := Grant{User: User{OpenID: "ou_alice", Name: "Alice"}, AccessToken: "u-1", RefreshToken: "r-1"}
	if _, err := store.Save(first, now); err != nil {
		t.Fatal(err)
	}
	second := Grant{User: User{OpenID: "ou_alice", Name: "Alice"}, AccessToken: "u-2", RefreshToken: "r-2"}
	if _, err := store.Save(second, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	stored, found, err := store.Load("ou_alice")
	if err != nil || !found {
		t.Fatalf("Load() found=%t, err=%v", found, err)
	}
	if stored.AccessToken != "u-2" || stored.RefreshToken != "r-2" {
		t.Fatalf("stored = %+v", stored)
	}
	if !stored.UpdatedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("updated at = %s", stored.UpdatedAt)
	}
}

func TestTokenStoreRejectsUnusableOpenID(t *testing.T) {
	t.Parallel()
	store, err := NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(Grant{User: User{Name: "Nobody"}}, time.Now()); err == nil {
		t.Fatal("Save() accepted a blank open_id")
	}
	if _, err := store.Save(Grant{User: User{OpenID: "../escape", Name: "Nobody"}}, time.Now()); err == nil {
		t.Fatal("Save() accepted an open_id with path separators")
	}
}

func TestTokenStoreLoadReportsMissingPerson(t *testing.T) {
	t.Parallel()
	store, err := NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stored, found, err := store.Load("ou_never_signed_in")
	if err != nil || found {
		t.Fatalf("Load() = %+v, found=%t, err=%v", stored, found, err)
	}
}

func TestNewTokenStoreRequiresDirectory(t *testing.T) {
	t.Parallel()
	if _, err := NewTokenStore("  "); err == nil {
		t.Fatal("NewTokenStore() accepted a blank directory")
	}
}
