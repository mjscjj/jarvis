package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func userTokensFixture(t *testing.T, provider Provider) (*UserTokens, *TokenStore) {
	t.Helper()
	store := authTestTokenStore(t)
	tokens, err := NewUserTokens(store, provider)
	if err != nil {
		t.Fatal(err)
	}
	return tokens, store
}

func TestUserTokensKeepsTokenThatIsStillValid(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	provider := &fakeProvider{}
	tokens, store := userTokensFixture(t, provider)
	tokens.now = func() time.Time { return now }
	if _, err := store.Save(Grant{
		User:         User{OpenID: "ou_alice", Name: "Alice"},
		AccessToken:  "u-1",
		RefreshToken: "r-1",
		ExpiresIn:    time.Hour,
	}, now); err != nil {
		t.Fatal(err)
	}
	stored, err := tokens.Ensure(context.Background(), "ou_alice")
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "u-1" {
		t.Fatalf("stored = %+v", stored)
	}
	if provider.refreshCalls != 0 {
		t.Fatalf("refresh calls = %d, want 0", provider.refreshCalls)
	}
}

func TestUserTokensRefreshesExpiringTokenAndRewritesFile(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	provider := &fakeProvider{refreshed: Grant{
		User:        User{OpenID: "ou_alice", Name: "Alice"},
		AccessToken: "u-2",
		ExpiresIn:   2 * time.Hour,
	}}
	tokens, store := userTokensFixture(t, provider)
	tokens.now = func() time.Time { return now }
	refreshDeadline := now.Add(1440 * time.Hour)
	if _, err := store.Save(Grant{
		User:             User{OpenID: "ou_alice", Name: "Alice"},
		AccessToken:      "u-1",
		RefreshToken:     "r-1",
		ExpiresIn:        time.Minute, // inside the refresh leeway
		RefreshExpiresIn: 1440 * time.Hour,
	}, now); err != nil {
		t.Fatal(err)
	}

	stored, err := tokens.Ensure(context.Background(), "ou_alice")
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "u-2" || provider.refreshCalls != 1 {
		t.Fatalf("stored = %+v, refresh calls = %d", stored, provider.refreshCalls)
	}
	// Feishu returned no new refresh token, so the old one and its deadline
	// must survive; otherwise the next refresh has nothing to present.
	if stored.RefreshToken != "r-1" {
		t.Fatalf("refresh token = %q, want r-1", stored.RefreshToken)
	}
	if stored.RefreshExpiresAt == nil || !stored.RefreshExpiresAt.Equal(refreshDeadline) {
		t.Fatalf("refresh deadline = %v, want %s", stored.RefreshExpiresAt, refreshDeadline)
	}
	onDisk, found, err := store.Load("ou_alice")
	if err != nil || !found {
		t.Fatalf("Load() found=%t, err=%v", found, err)
	}
	if onDisk.AccessToken != "u-2" || onDisk.RefreshToken != "r-1" {
		t.Fatalf("on-disk record = %+v", onDisk)
	}
	if onDisk.RefreshExpiresAt == nil || !onDisk.RefreshExpiresAt.Equal(refreshDeadline) {
		t.Fatalf("on-disk refresh deadline = %v, want %s", onDisk.RefreshExpiresAt, refreshDeadline)
	}
}

func TestUserTokensReportsUnusableGrantsInsteadOfGuessing(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)

	t.Run("never signed in", func(t *testing.T) {
		tokens, _ := userTokensFixture(t, &fakeProvider{})
		tokens.now = func() time.Time { return now }
		if _, err := tokens.Ensure(context.Background(), "ou_nobody"); !errors.Is(err, ErrNoUserToken) {
			t.Fatalf("Ensure() error = %v, want ErrNoUserToken", err)
		}
	})

	t.Run("expired with no refresh token", func(t *testing.T) {
		tokens, store := userTokensFixture(t, &fakeProvider{})
		tokens.now = func() time.Time { return now }
		if _, err := store.Save(Grant{
			User:        User{OpenID: "ou_alice", Name: "Alice"},
			AccessToken: "u-1",
			ExpiresIn:   time.Second,
		}, now); err != nil {
			t.Fatal(err)
		}
		if _, err := tokens.Ensure(context.Background(), "ou_alice"); !errors.Is(err, ErrUserTokenUnusable) {
			t.Fatalf("Ensure() error = %v, want ErrUserTokenUnusable", err)
		}
	})

	t.Run("refresh token rejected by Feishu", func(t *testing.T) {
		provider := &fakeProvider{refreshErr: ErrDeviceAuthorizationExpired}
		tokens, store := userTokensFixture(t, provider)
		tokens.now = func() time.Time { return now }
		if _, err := store.Save(Grant{
			User:         User{OpenID: "ou_alice", Name: "Alice"},
			AccessToken:  "u-1",
			RefreshToken: "r-1",
			ExpiresIn:    time.Second,
		}, now); err != nil {
			t.Fatal(err)
		}
		if _, err := tokens.Ensure(context.Background(), "ou_alice"); !errors.Is(err, ErrUserTokenUnusable) {
			t.Fatalf("Ensure() error = %v, want ErrUserTokenUnusable", err)
		}
	})

	t.Run("refresh returns a different person", func(t *testing.T) {
		provider := &fakeProvider{refreshed: Grant{
			User:        User{OpenID: "ou_bob", Name: "Bob"},
			AccessToken: "u-bob",
			ExpiresIn:   time.Hour,
		}}
		tokens, store := userTokensFixture(t, provider)
		tokens.now = func() time.Time { return now }
		if _, err := store.Save(Grant{
			User:         User{OpenID: "ou_alice", Name: "Alice"},
			AccessToken:  "u-1",
			RefreshToken: "r-1",
			ExpiresIn:    time.Second,
		}, now); err != nil {
			t.Fatal(err)
		}
		_, err := tokens.Ensure(context.Background(), "ou_alice")
		if err == nil {
			t.Fatal("Ensure() accepted a grant belonging to someone else")
		}
		onDisk, _, loadErr := store.Load("ou_alice")
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if onDisk.AccessToken != "u-1" {
			t.Fatalf("Alice's file was overwritten with %+v", onDisk)
		}
	})
}

func TestNewUserTokensRequiresStoreAndProvider(t *testing.T) {
	t.Parallel()
	if _, err := NewUserTokens(nil, &fakeProvider{}); err == nil {
		t.Fatal("NewUserTokens() accepted a nil store")
	}
	if _, err := NewUserTokens(authTestTokenStore(t), nil); err == nil {
		t.Fatal("NewUserTokens() accepted a nil provider")
	}
}
