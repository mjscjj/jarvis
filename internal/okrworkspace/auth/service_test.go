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
	user          User
	pollErrors    []error
	pollCalls     int
}

func (f *fakeProvider) RequestDeviceAuthorization(context.Context) (DeviceAuthorization, error) {
	return f.authorization, nil
}

func (f *fakeProvider) PollDeviceAuthorization(context.Context, string) (User, error) {
	f.pollCalls++
	if len(f.pollErrors) > 0 {
		err := f.pollErrors[0]
		f.pollErrors = f.pollErrors[1:]
		return User{}, err
	}
	return f.user, nil
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
		user: User{OpenID: "ou_alice", Name: "Alice"},
	}
	service, err := NewService(authTestDB(t), moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, provider)
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
	current, err := service.Current(context.Background(), token)
	if err != nil || current.User.Name != "Alice" {
		t.Fatalf("Current() = %+v, %v", current, err)
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
	service, err := NewService(authTestDB(t), moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, provider)
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
	service, err := NewService(authTestDB(t), moduleconfig.IdentityConfig{}, nil)
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
