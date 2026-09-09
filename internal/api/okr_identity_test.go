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

	"github.com/cloudwego/hertz/pkg/app"
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

func TestOKRPlanAccessUsesEnterpriseEmailAllowlist(t *testing.T) {
	if len(okrPlanViewers) != 9 {
		t.Fatalf("viewer count = %d, want 9", len(okrPlanViewers))
	}
	for _, viewer := range okrPlanViewers {
		if access := okrPlanAccessForUser(okrAuth.User{Name: viewer.Name, UnionID: viewer.UnionID}, true); access != okrPlanAccessViewer {
			t.Fatalf("%s union_id access = %q, want viewer", viewer.Name, access)
		}
		if access := okrPlanAccessForUser(okrAuth.User{Name: viewer.Name, Email: "  " + strings.ToUpper(viewer.Email) + "  "}, true); access != okrPlanAccessViewer {
			t.Fatalf("%s <%s> access = %q, want viewer", viewer.Name, viewer.Email, access)
		}
	}
	if access := okrPlanAccessForUser(okrAuth.User{UnionID: okrPlanEditorUnionID}, true); access != okrPlanAccessEditor {
		t.Fatalf("editor access = %q", access)
	}
	if access := okrPlanAccessForUser(okrAuth.User{Name: "同名人员", Email: "someone@bytedance.com"}, true); access != okrPlanAccessNone {
		t.Fatalf("unlisted access = %q", access)
	}
	if access := okrPlanAccessForUser(jarvisOKRUser, false); access != okrPlanAccessEditor {
		t.Fatalf("disabled identity access = %q", access)
	}
}

func TestRequireOKRPlanAccessSeparatesViewersAndEditor(t *testing.T) {
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
	addOKRIdentitySession(t, db, "viewer", okrPlanViewers[0].UnionID, "")
	addOKRIdentitySession(t, db, "editor", okrPlanEditorUnionID, "")
	addOKRIdentitySession(t, db, "outsider", "on_outsider", "outsider@bytedance.com")

	h := server.Default()
	ok := func(_ context.Context, c *app.RequestContext) { c.String(consts.StatusOK, "ok") }
	h.GET("/plan", RequireOKRPlanAccess(identity, okrPlanAccessViewer), ok)
	h.POST("/plan", RequireOKRPlanAccess(identity, okrPlanAccessEditor), ok)
	h.GET("/me", GetOKRCurrentUser(identity))
	cookie := func(token string) ut.Header { return ut.Header{Key: "Cookie", Value: okrAuth.CookieName + "=" + token} }

	for _, test := range []struct {
		name, method, token string
		want                int
	}{
		{name: "anonymous viewer", method: "GET", want: consts.StatusUnauthorized},
		{name: "outsider viewer", method: "GET", token: "outsider", want: consts.StatusForbidden},
		{name: "allowlisted viewer", method: "GET", token: "viewer", want: consts.StatusOK},
		{name: "viewer cannot edit", method: "POST", token: "viewer", want: consts.StatusForbidden},
		{name: "editor can edit", method: "POST", token: "editor", want: consts.StatusOK},
	} {
		headers := []ut.Header(nil)
		if test.token != "" {
			headers = append(headers, cookie(test.token))
		}
		response := ut.PerformRequest(h.Engine, test.method, "/plan", nil, headers...).Result()
		if response.StatusCode() != test.want {
			t.Fatalf("%s: status=%d body=%s", test.name, response.StatusCode(), response.Body())
		}
	}

	response := ut.PerformRequest(h.Engine, "GET", "/me", nil, cookie("viewer")).Result()
	var payload struct {
		Data okrCurrentUserResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.PlanAccess != okrPlanAccessViewer {
		t.Fatalf("me plan_access = %q, want viewer", payload.Data.PlanAccess)
	}
}
