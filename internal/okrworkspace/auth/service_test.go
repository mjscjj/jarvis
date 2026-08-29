package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeProvider struct{ user User }

func (f fakeProvider) AuthorizationURL(state string) string {
	return "https://accounts.example/authorize?state=" + url.QueryEscape(state)
}
func (f fakeProvider) ExchangeCode(context.Context, string) (User, error) { return f.user, nil }

func authTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.OAuthState{}, &domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestServiceCompletesIdentityOnlySession(t *testing.T) {
	cfg := moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}
	service, err := NewService(authTestDB(t), cfg, fakeProvider{user: User{OpenID: "ou_alice", Name: "Alice"}})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 8, 28, 8, 0, 0, 0, time.UTC) }

	location, err := service.BeginLogin(context.Background(), "/#/okr?week=2026-W35")
	if err != nil {
		t.Fatalf("BeginLogin() error = %v", err)
	}
	parsed, _ := url.Parse(location)
	state := parsed.Query().Get("state")
	session, token, returnTo, err := service.CompleteLogin(context.Background(), "one-time-code", state)
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	if session.User.OpenID != "ou_alice" || token == "" || returnTo != "/#/okr?week=2026-W35" {
		t.Fatalf("session=%+v token=%q returnTo=%q", session, token, returnTo)
	}
	current, err := service.Current(context.Background(), token)
	if err != nil || current.User.Name != "Alice" {
		t.Fatalf("Current() = %+v, %v", current, err)
	}
	if _, _, _, err := service.CompleteLogin(context.Background(), "code", state); err == nil {
		t.Fatal("OAuth state was reusable")
	}
	if err := service.Logout(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Current(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Current() after logout error = %v", err)
	}
}

func TestServiceDisabledHasNoSession(t *testing.T) {
	service, err := NewService(authTestDB(t), moduleconfig.IdentityConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Current(context.Background(), "anything"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Current() error = %v", err)
	}
}

func TestServiceRejectsOpenRedirect(t *testing.T) {
	service, err := NewService(authTestDB(t), moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, fakeProvider{user: User{OpenID: "ou_1", Name: "User"}})
	if err != nil {
		t.Fatal(err)
	}
	location, err := service.BeginLogin(context.Background(), "https://evil.example/path")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(location)
	_, _, returnTo, err := service.CompleteLogin(context.Background(), "code", parsed.Query().Get("state"))
	if err != nil {
		t.Fatal(err)
	}
	if returnTo != defaultReturnTo {
		t.Fatalf("unsafe return URL accepted: %q", returnTo)
	}
}
