package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeProvider struct {
	authorization DeviceAuthorization
	grant         Grant
	pollErrors    []error
	pollCalls     int
	refreshed     Grant
	refreshErr    error
	refreshCalls  int
}

func (f *fakeProvider) RequestDeviceAuthorization(context.Context) (DeviceAuthorization, error) {
	return f.authorization, nil
}

func (f *fakeProvider) PollDeviceAuthorization(context.Context, string) (Grant, error) {
	f.pollCalls++
	if len(f.pollErrors) > 0 {
		err := f.pollErrors[0]
		f.pollErrors = f.pollErrors[1:]
		return Grant{}, err
	}
	return f.grant, nil
}

func (f *fakeProvider) RefreshGrant(context.Context, string) (Grant, error) {
	f.refreshCalls++
	if f.refreshErr != nil {
		return Grant{}, f.refreshErr
	}
	return f.refreshed, nil
}

func authTestTokenStore(t *testing.T) *TokenStore {
	t.Helper()
	store, err := NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func authTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestServiceCompletesDeviceIdentitySession(t *testing.T) {
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	provider := &fakeProvider{
		authorization: DeviceAuthorization{
			DeviceCode: "device-secret", VerificationURL: "https://accounts.example/device", UserCode: "ABCD-1234",
			ExpiresIn: 10 * time.Minute, PollInterval: 5 * time.Second,
		},
		grant: Grant{
			User:             User{OpenID: "ou_alice", UnionID: "on_alice", Name: "Alice", Email: "alice@example.com"},
			AccessToken:      "u-alice",
			RefreshToken:     "r-alice",
			TokenType:        "Bearer",
			Scope:            "offline_access",
			ExpiresIn:        2 * time.Hour,
			RefreshExpiresIn: 60 * 24 * time.Hour,
		},
	}
	tokens := authTestTokenStore(t)
	service, err := NewService(authTestDB(t), moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, provider, tokens)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }

	login, err := service.BeginDeviceLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if login.ID == "" || login.UserCode != "ABCD-1234" || login.PollIntervalSeconds != 5 {
		t.Fatalf("login = %+v", login)
	}
	poll, _, err := service.PollDeviceLogin(context.Background(), login.ID)
	if err != nil || poll.Status != DeviceLoginPending || provider.pollCalls != 0 {
		t.Fatalf("early poll = %+v, %v; calls=%d", poll, err, provider.pollCalls)
	}

	now = now.Add(5 * time.Second)
	poll, token, err := service.PollDeviceLogin(context.Background(), login.ID)
	if err != nil || poll.Status != DeviceLoginCompleted || token == "" || poll.User == nil || poll.User.OpenID != "ou_alice" {
		t.Fatalf("completed poll = %+v, token=%q, err=%v", poll, token, err)
	}
	stored, found, err := tokens.Load("ou_alice")
	if err != nil || !found {
		t.Fatalf("stored token: found=%t, err=%v", found, err)
	}
	if stored.AccessToken != "u-alice" || stored.RefreshToken != "r-alice" || stored.Name != "Alice" || stored.Email != "alice@example.com" {
		t.Fatalf("stored token = %+v", stored)
	}
	if stored.UnionID != "on_alice" {
		t.Fatalf("stored union_id = %q, want on_alice", stored.UnionID)
	}
	if stored.ExpiresAt == nil || !stored.ExpiresAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("stored access token expiry = %v, want %s", stored.ExpiresAt, now.Add(2*time.Hour))
	}
	if stored.RefreshExpiresAt == nil || !stored.RefreshExpiresAt.Equal(now.Add(60*24*time.Hour)) {
		t.Fatalf("stored refresh token expiry = %v, want %s", stored.RefreshExpiresAt, now.Add(60*24*time.Hour))
	}
	current, err := service.Current(context.Background(), token)
	if err != nil || current.User.Name != "Alice" || current.User.UnionID != "on_alice" {
		t.Fatalf("Current() = %+v, %v", current, err)
	}
	if want := now.Add(24 * time.Hour); !current.ExpiresAt.Equal(want) {
		t.Fatalf("session expiry = %s, want %s", current.ExpiresAt, want)
	}
	if _, _, err := service.PollDeviceLogin(context.Background(), login.ID); !errors.Is(err, ErrDeviceLoginNotFound) {
		t.Fatalf("completed login was reusable: %v", err)
	}
	if err := service.Logout(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Current(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Current() after logout error = %v", err)
	}
}

func TestServiceHandlesDevicePendingSlowDownAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	provider := &fakeProvider{
		authorization: DeviceAuthorization{DeviceCode: "d", VerificationURL: "https://accounts.example/device", ExpiresIn: time.Minute, PollInterval: time.Second},
		pollErrors:    []error{ErrDeviceAuthorizationSlowDown, ErrDeviceAuthorizationPending},
	}
	service, err := NewService(authTestDB(t), moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, provider, authTestTokenStore(t))
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	login, err := service.BeginDeviceLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	poll, _, err := service.PollDeviceLogin(context.Background(), login.ID)
	if err != nil || poll.Status != DeviceLoginPending || poll.RetryAfterSeconds != 6 {
		t.Fatalf("slow-down poll = %+v, %v", poll, err)
	}
	now = login.ExpiresAt
	poll, _, err = service.PollDeviceLogin(context.Background(), login.ID)
	if err != nil || poll.Status != DeviceLoginExpired {
		t.Fatalf("expired poll = %+v, %v", poll, err)
	}
}

func TestServiceDisabledHasNoSessionOrLogin(t *testing.T) {
	service, err := NewService(authTestDB(t), moduleconfig.IdentityConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Current(context.Background(), "anything"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Current() error = %v", err)
	}
	if _, err := service.BeginDeviceLogin(context.Background()); err == nil {
		t.Fatal("BeginDeviceLogin() succeeded while disabled")
	}
}
