package api

import (
	"context"
	"encoding/json"
	"testing"

	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
