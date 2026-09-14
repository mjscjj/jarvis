package authn

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSessionsSurviveDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	now := time.Now().UTC().Truncate(time.Second)
	runner := fakeRunner{run: func(string, []string) ([]byte, error) {
		t.Fatal("restoring a session must not invoke SSO")
		return nil, nil
	}}
	open := func(allowed []string) (*Service, func()) {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		s, err := NewServiceWithRunner(db, "bytedcli", 12*time.Hour, true, allowed, runner)
		if err != nil {
			t.Fatal(err)
		}
		s.now = func() time.Time { return now }
		return s, func() {
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	s, closeDB := open([]string{"alice", "bob"})
	tokens := make([]string, 3)
	for i, name := range []string{"alice", "alice", "bob"} {
		result, err := s.startSession(User{Username: name, Email: name + "@bytedance.com", IsPrincipal: true})
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = result.SessionToken
	}
	var rows []session
	if err := s.db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("saved sessions = %d", len(rows))
	}
	for i, row := range rows {
		for _, token := range tokens {
			if row.TokenHash == token {
				t.Fatal("raw cookie persisted")
			}
		}
		if !row.ExpiresAt.Equal(now.Add(12 * time.Hour)) {
			t.Fatalf("session %d expiry changed", i)
		}
	}
	closeDB()
	now = now.Add(time.Hour)
	s, closeDB = open([]string{"alice"})
	for _, token := range tokens[:2] {
		user, ok := s.Authenticate(token)
		if !ok || user.Username != "alice" || !user.IsPrincipal {
			t.Fatal("original browser cookie did not survive restart")
		}
	}
	if _, ok := s.Authenticate(tokens[2]); ok {
		t.Fatal("removed principal still has access")
	}
	if _, ok := s.Authenticate(tokens[0] + "invalid"); ok {
		t.Fatal("unknown cookie accepted")
	}
	if err := s.Logout(tokens[0]); err != nil {
		t.Fatal(err)
	}
	closeDB()
	s, closeDB = open([]string{"alice"})
	if _, ok := s.Authenticate(tokens[0]); ok {
		t.Fatal("logout was undone by restart")
	}
	if _, ok := s.Authenticate(tokens[1]); !ok {
		t.Fatal("logout revoked another browser")
	}
	closeDB()
	now = now.Add(11 * time.Hour)
	s, _ = open([]string{"alice"})
	if _, ok := s.Authenticate(tokens[1]); ok {
		t.Fatal("restart extended the original expiry")
	}
}

func TestSessionDatabaseFailureDoesNotIssueCookieOrReportLogoutSuccess(t *testing.T) {
	s := newTestService(t, fakeRunner{})
	sqlDB, err := s.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := s.startSession(User{Username: "alice"})
	if err == nil || result.SessionToken != "" {
		t.Fatal("session issued without durable storage")
	}
	if err := s.Logout("some-cookie"); err == nil {
		t.Fatal("failed logout reported success")
	}
	if _, ok := s.Authenticate("some-cookie"); ok {
		t.Fatal("database failure granted access")
	}
}
