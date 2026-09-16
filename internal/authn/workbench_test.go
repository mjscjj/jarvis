package authn

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"
)

func TestWorkbenchPreferenceUsesVerifiedIdentityWithoutGrantingAccess(t *testing.T) {
	service := newTestService(t, fakeRunner{})
	service.SetMainWorkbenchAccounts([]string{" LIXIAOLIN "})
	if err := service.db.AutoMigrate(&domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	tokens, err := okrAuth.NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Current() reads sessions only; a provider call would fail this test.
	identity, err := okrAuth.NewService(service.db, moduleconfig.IdentityConfig{Enabled: true}, struct{ okrAuth.Provider }{}, tokens)
	if err != nil {
		t.Fatal(err)
	}
	service.SetOKRIdentity(identity)
	unionID := "on_test_main_user"
	service.SetOKRAccountBindings(map[string]User{unionID: {Username: "lixiaolin", Email: "claire.li@bytedance.com"}})
	for _, tc := range []struct {
		name, unionID      string
		expired, preferred bool
	}{
		{"verified-main-user", unionID, false, true},
		{"same-email-other-identity", "unknown-union", false, false},
		{"expired-session", unionID, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expires := time.Now().Add(time.Hour)
			if tc.expired {
				expires = time.Now().Add(-time.Hour)
			}
			row := domain.AuthSession{TokenHash: fmt.Sprintf("%x", sha256.Sum256([]byte(tc.name))), OpenID: tc.name, UnionID: tc.unionID, Email: "claire.li@bytedance.com", Name: "same display name", ExpiresAt: expires, LastSeenAt: time.Now()}
			if err := service.db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			request := requestContext("10.0.0.8:43000", map[string]string{"Cookie": okrAuth.CookieName + "=" + tc.name, "Sec-Fetch-Mode": "cors"})
			view := service.RequestStatus(t.Context(), request)
			if view.PreferMainWorkbench != tc.preferred || view.User != nil || view.Status != StatusUnauthenticated {
				t.Fatalf("navigation preference changed principal access: %+v", view)
			}
			if _, allowed := service.AuthenticateRequest(t.Context(), request); allowed {
				t.Fatal("preference granted private API access")
			}
		})
	}
	service.SetOKRAccountBindings(nil)
	removed := requestContext("10.0.0.8:43000", map[string]string{"Cookie": okrAuth.CookieName + "=verified-main-user"})
	if view := service.RequestStatus(t.Context(), removed); view.PreferMainWorkbench || view.User != nil {
		t.Fatalf("removed binding retained a built-in account: %+v", view)
	}
	request := requestContextForPath("10.0.0.8:43000", "/api/auth/status?email=claire.li@bytedance.com&prefer_main_workbench=true", nil)
	if service.RequestStatus(t.Context(), request).PreferMainWorkbench {
		t.Fatal("query selected an identity")
	}
}

func TestWorkbenchPreferenceKeepsDeveloperLocal(t *testing.T) {
	service := newTestService(t, fakeRunner{})
	service.SetMainWorkbenchAccounts([]string{"lixiaolin"})
	login, err := service.startSession(User{Username: "alice", Email: "alice@bytedance.com", IsPrincipal: true})
	if err != nil {
		t.Fatal(err)
	}
	service.SetBrowserCookie("jarvis_dev_session", "/dev/")
	request := requestContext("10.0.0.8:43000", map[string]string{"Cookie": service.CookieName() + "=" + login.SessionToken})
	view := service.RequestStatus(t.Context(), request)
	if view.PreferMainWorkbench || view.User == nil {
		t.Fatalf("developer behavior changed: %+v", view)
	}
	service.SetMainWorkbenchAccounts([]string{" ALICE@BYTEDANCE.COM "})
	if !service.RequestStatus(t.Context(), request).PreferMainWorkbench {
		t.Fatal("verified account email did not match")
	}
}
