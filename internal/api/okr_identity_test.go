package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/authn"
	mainDomain "jarvis/internal/domain"
	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"
	"jarvis/internal/security"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func addOKRIdentitySession(t *testing.T, db *gorm.DB, token, unionID, email string) {
	t.Helper()
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(token))
	if err := db.Create(&domain.AuthSession{
		TokenHash: hex.EncodeToString(digest[:]),
		OpenID:    "ou_" + token, UnionID: unionID, Name: token, Email: email,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

type okrIdentityProviderStub struct{}

func (okrIdentityProviderStub) RequestDeviceAuthorization(context.Context) (okrAuth.DeviceAuthorization, error) {
	return okrAuth.DeviceAuthorization{}, nil
}

func (okrIdentityProviderStub) PollDeviceAuthorization(context.Context, string) (okrAuth.Grant, error) {
	return okrAuth.Grant{}, nil
}

func (okrIdentityProviderStub) RefreshGrant(context.Context, string) (okrAuth.Grant, error) {
	return okrAuth.Grant{}, nil
}

func TestPollOKRFeishuDeviceLoginTreatsLostLoginAsExpired(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	tokens, err := okrAuth.NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, okrIdentityProviderStub{}, tokens)
	if err != nil {
		t.Fatal(err)
	}
	h := server.Default()
	h.POST("/api/biz-okr/auth/feishu/device/:login_id/poll", PollOKRFeishuDeviceLogin(identity))

	response := ut.PerformRequest(h.Engine, "POST", "/api/biz-okr/auth/feishu/device/lost-after-restart/poll", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data okrAuth.DeviceLoginPoll `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Status != okrAuth.DeviceLoginExpired {
		t.Fatalf("poll=%+v, want expired", payload.Data)
	}
}

func TestOKRPrincipalBrowserIdentity(t *testing.T) {
	db := openAuthTestDB(t)
	if err := db.AutoMigrate(&domain.AuthSession{}, &mainDomain.AccessAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	tokens, err := okrAuth.NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, okrIdentityProviderStub{}, tokens)
	if err != nil {
		t.Fatal(err)
	}
	service, err := authn.NewServiceWithRunner(db, "bytedcli", time.Hour, true, []string{"chujiejie.1", "lixiaolin"}, authRunner{run: func(string, []string) ([]byte, error) {
		t.Fatal("OKR identity must not invoke SSO")
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	service.SetOKRIdentity(identity)
	addOKRIdentitySession(t, db, "chu", "on_94b5aa46ca92b7aecd01031e5b2f0dc4", "")
	addOKRIdentitySession(t, db, "li", "on_af023f3c29b03b3d90cffedbc703b005", "")
	addOKRIdentitySession(t, db, "other", "on_outsider", "chujiejie.1@bytedance.com")
	addOKRIdentitySession(t, db, "expired", "on_94b5aa46ca92b7aecd01031e5b2f0dc4", "")
	if err := db.Model(&domain.AuthSession{}).Where("name = ?", "expired").Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO browser_auth_session (token_hash, username, email, expires_at) VALUES (?, ?, ?, ?)", fmt.Sprintf("%x", sha256.Sum256([]byte("sso"))), "lixiaolin", "claire.li@bytedance.com", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	audit, err := security.NewAuditService(db)
	if err != nil {
		t.Fatal(err)
	}
	h := server.Default()
	h.Use(audit.Middleware(service), authn.BrowserMiddleware(service))
	h.GET("/api/auth/status", GetAuthStatus(service))
	h.POST("/api/auth/logout", LogoutFromJarvis(service))
	h.POST("/api/biz-okr/auth/logout", LogoutOKR(identity, service))
	h.GET("/api/biz-okr/me", GetOKRCurrentUser(identity))
	h.GET("/api/chat/sessions", func(_ context.Context, c *app.RequestContext) { c.JSON(200, map[string]any{"items": []any{}}) })
	request := func(method, path, cookies string) *protocol.Response {
		return ut.PerformRequest(h.Engine, method, path, nil, ut.Header{Key: "Cookie", Value: cookies}, ut.Header{Key: "Sec-Fetch-Mode", Value: "cors"}).Result()
	}
	for _, tc := range []struct{ name, cookies, username string }{
		{"first principal", "jarvis_okr_session=chu", "chujiejie.1"},
		{"second principal", "jarvis_okr_session=li", "lixiaolin"},
		{"SSO alone", "jarvis_session=sso", "lixiaolin"},
		{"ordinary with principal email", "jarvis_okr_session=other", ""},
		{"unknown cookie", "jarvis_okr_session=unknown", ""},
		{"expired cookie", "jarvis_okr_session=expired", ""},
		{"ordinary masks old SSO", "jarvis_okr_session=other; jarvis_session=sso", ""},
		{"OKR wins account conflict", "jarvis_okr_session=chu; jarvis_session=sso", "chujiejie.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := request("GET", "/api/auth/status", tc.cookies)
			var result struct {
				Data authn.View `json:"data"`
			}
			if err := json.Unmarshal(response.Body(), &result); err != nil {
				t.Fatal(err)
			}
			if tc.username == "" {
				if result.Data.User != nil || result.Data.Status != authn.StatusUnauthenticated {
					t.Fatalf("unexpected identity: %+v", result.Data)
				}
			} else if result.Data.User == nil || result.Data.User.Username != tc.username {
				t.Fatalf("identity: %+v", result.Data)
			}
			want := 401
			if tc.username != "" {
				want = 200
			}
			if got := request("GET", "/api/chat/sessions", tc.cookies).StatusCode(); got != want {
				t.Fatalf("chat status = %d, want %d", got, want)
			}
		})
	}
	if got := request("GET", "/api/chat/sessions", "jarvis_session=sso").StatusCode(); got != 401 {
		t.Fatalf("superseded SSO status = %d, want 401", got)
	}
	items, err := audit.List(t.Context(), security.AuditFilter{ActorKind: "principal"})
	if err != nil {
		t.Fatal(err)
	}
	actors := map[string]bool{}
	for _, item := range items.Items {
		actors[item.ActorID] = true
	}
	if !actors["chujiejie.1@bytedance.com"] || !actors["claire.li@bytedance.com"] {
		t.Fatalf("audit actors: %v", actors)
	}
	for _, path := range []string{"/api/auth/logout", "/api/biz-okr/auth/logout"} {
		addOKRIdentitySession(t, db, "logout", "on_94b5aa46ca92b7aecd01031e5b2f0dc4", "")
		if err := db.Exec("INSERT OR REPLACE INTO browser_auth_session (token_hash, username, email, expires_at) VALUES (?, ?, ?, ?)", fmt.Sprintf("%x", sha256.Sum256([]byte("sso"))), "lixiaolin", "claire.li@bytedance.com", time.Now().Add(time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		response := request("POST", path, "jarvis_okr_session=logout; jarvis_session=sso")
		if response.StatusCode() != 200 {
			t.Fatalf("logout: %s", response.Body())
		}
		for _, cookie := range []string{"jarvis_okr_session=logout", "jarvis_session=sso"} {
			if request("GET", "/api/chat/sessions", cookie).StatusCode() != 401 {
				t.Fatal("logout left valid credentials")
			}
		}
		if path == "/api/biz-okr/auth/logout" && !strings.Contains(string(response.Body()), `"logged_out":true`) {
			t.Fatal("OKR logout response changed")
		}
		if path == "/api/biz-okr/auth/logout" {
			items, err := audit.List(t.Context(), security.AuditFilter{ActorKind: "principal", Route: path})
			if err != nil {
				t.Fatal(err)
			}
			if len(items.Items) == 0 || items.Items[0].ActorID != "chujiejie.1@bytedance.com" {
				t.Fatalf("logout audit actor: %+v", items.Items)
			}
		}
	}
	if _, err := identity.Current(t.Context(), "li"); err != nil {
		t.Fatal("logout revoked another browser")
	}
	removed, err := authn.NewServiceWithRunner(db, "bytedcli", time.Hour, true, []string{"someone-else"}, authRunner{})
	if err != nil {
		t.Fatal(err)
	}
	removed.SetOKRIdentity(identity)
	c := authRequestContext("127.0.0.1", "")
	c.Request.Header.SetCookie(okrAuth.CookieName, "li")
	if _, ok := removed.AuthenticateRequest(t.Context(), c); ok {
		t.Fatal("removed principal retained OKR access")
	}
}

func TestOKRManagementAccessUsesEnterpriseIdentityAllowlist(t *testing.T) {
	if len(okrManagementUsers) != 10 {
		t.Fatalf("management user count = %d, want 10", len(okrManagementUsers))
	}
	for _, manager := range okrManagementUsers {
		if !canManageOKR(okrAuth.User{Name: manager.Name, UnionID: manager.UnionID}, true) {
			t.Fatalf("%s union_id should have management access", manager.Name)
		}
		if !canManageOKR(okrAuth.User{Name: manager.Name, Email: "  " + strings.ToUpper(manager.Email) + "  "}, true) {
			t.Fatalf("%s <%s> should have management access", manager.Name, manager.Email)
		}
	}
	if !canManageOKR(okrAuth.User{UnionID: okrPlanEditorUnionID}, true) {
		t.Fatal("principal should have management access")
	}
	if canManageOKR(okrAuth.User{Name: "同名人员", Email: "someone@bytedance.com"}, true) {
		t.Fatal("unlisted user should not have management access")
	}
	if !canManageOKR(jarvisOKRUser, false) {
		t.Fatal("disabled identity should preserve local management access")
	}
}

func TestOKRCurrentUserReportsManagementAccess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	tokens, err := okrAuth.NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, okrIdentityProviderStub{}, tokens)
	if err != nil {
		t.Fatal(err)
	}
	addOKRIdentitySession(t, db, "manager", okrManagementUsers[0].UnionID, "")
	addOKRIdentitySession(t, db, "ruoyi-union", "on_833914b05fbbe2eb6623865af52d984f", "")
	addOKRIdentitySession(t, db, "ruoyi-email", "on_other_provider", "ruoyizhang@bytedance.com")
	addOKRIdentitySession(t, db, "outsider", "on_outsider", "outsider@bytedance.com")

	h := server.Default()
	h.GET("/me", GetOKRCurrentUser(identity))
	cookie := func(token string) ut.Header { return ut.Header{Key: "Cookie", Value: okrAuth.CookieName + "=" + token} }

	for _, test := range []struct {
		name, token    string
		wantManagement bool
		wantAutoMatch  bool
	}{
		{name: "allowlisted manager", token: "manager", wantManagement: true},
		{name: "Ruoyi with existing email-less login", token: "ruoyi-union", wantManagement: true, wantAutoMatch: true},
		{name: "Ruoyi by enterprise email", token: "ruoyi-email", wantManagement: true, wantAutoMatch: true},
		{name: "unlisted user", token: "outsider"},
	} {
		response := ut.PerformRequest(h.Engine, "GET", "/me", nil, cookie(test.token)).Result()
		if response.StatusCode() != consts.StatusOK {
			t.Fatalf("%s: status=%d body=%s", test.name, response.StatusCode(), response.Body())
		}
		var payload struct {
			Data okrCurrentUserResponse `json:"data"`
		}
		if err := json.Unmarshal(response.Body(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Data.ManagementAccess != test.wantManagement {
			t.Fatalf("%s management_access = %t, want %t", test.name, payload.Data.ManagementAccess, test.wantManagement)
		}
		if payload.Data.RegionalAutoMatchAccess != test.wantAutoMatch {
			t.Fatalf("%s regional_auto_match_access = %t, want %t", test.name, payload.Data.RegionalAutoMatchAccess, test.wantAutoMatch)
		}
	}
}
