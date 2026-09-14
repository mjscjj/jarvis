package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
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
		name, token string
		want        bool
	}{
		{name: "allowlisted manager", token: "manager", want: true},
		{name: "Ruoyi with existing email-less login", token: "ruoyi-union", want: true},
		{name: "Ruoyi by enterprise email", token: "ruoyi-email", want: true},
		{name: "unlisted user", token: "outsider", want: false},
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
		if payload.Data.ManagementAccess != test.want {
			t.Fatalf("%s management_access = %t, want %t", test.name, payload.Data.ManagementAccess, test.want)
		}
	}
}
