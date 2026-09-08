package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"jarvis/internal/config"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeSecuritySettingsService struct {
	view       *config.SecuritySettingsView
	updated    config.SecuritySettings
	updateErr  error
	callOrder  *[]string
	updateCall bool
}

func (f *fakeSecuritySettingsService) GetSecurity(context.Context) (*config.SecuritySettingsView, error) {
	return f.view, nil
}

func (f *fakeSecuritySettingsService) UpdateSecurity(_ context.Context, input config.SecuritySettings) (*config.SecuritySettingsView, error) {
	f.updated = input
	f.updateCall = true
	if f.callOrder != nil {
		*f.callOrder = append(*f.callOrder, "update")
	}
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return &config.SecuritySettingsView{Settings: input}, nil
}

type fakeP2PCheckpointAdvancer struct {
	err       error
	callOrder *[]string
}

func (f *fakeP2PCheckpointAdvancer) AdvanceP2PCheckpoints(context.Context) error {
	if f.callOrder != nil {
		*f.callOrder = append(*f.callOrder, "advance")
	}
	return f.err
}

func TestUpdateSecuritySettingsAdvancesCheckpointsBeforeEnabling(t *testing.T) {
	order := make([]string, 0, 2)
	service := &fakeSecuritySettingsService{
		view:      &config.SecuritySettingsView{Settings: config.SecuritySettings{P2PScanEnabled: false}},
		callOrder: &order,
	}
	advancer := &fakeP2PCheckpointAdvancer{callOrder: &order}
	h := server.New()
	h.PUT("/api/security-settings", UpdateSecuritySettings(service, advancer))
	body := []byte(`{"p2p_scan_enabled":true}`)

	response := ut.PerformRequest(
		h.Engine, "PUT", "/api/security-settings",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
	).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if len(order) != 2 || order[0] != "advance" || order[1] != "update" {
		t.Fatalf("call order = %v, want [advance update]", order)
	}
	if !service.updated.P2PScanEnabled {
		t.Fatalf("updated settings = %#v", service.updated)
	}
}

func TestUpdateSecuritySettingsDoesNotAdvanceWhenDisabling(t *testing.T) {
	order := make([]string, 0, 1)
	service := &fakeSecuritySettingsService{
		view:      &config.SecuritySettingsView{Settings: config.SecuritySettings{P2PScanEnabled: true}},
		callOrder: &order,
	}
	advancer := &fakeP2PCheckpointAdvancer{callOrder: &order}
	h := server.New()
	h.PUT("/api/security-settings", UpdateSecuritySettings(service, advancer))
	body := []byte(`{"p2p_scan_enabled":false}`)

	response := ut.PerformRequest(
		h.Engine, "PUT", "/api/security-settings",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
	).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if len(order) != 1 || order[0] != "update" {
		t.Fatalf("call order = %v, want [update]", order)
	}
}

func TestUpdateSecuritySettingsStopsWhenCheckpointAdvanceFails(t *testing.T) {
	service := &fakeSecuritySettingsService{
		view: &config.SecuritySettingsView{Settings: config.SecuritySettings{P2PScanEnabled: false}},
	}
	advancer := &fakeP2PCheckpointAdvancer{err: errors.New("checkpoint failed")}
	h := server.New()
	h.PUT("/api/security-settings", UpdateSecuritySettings(service, advancer))
	body := []byte(`{"p2p_scan_enabled":true}`)

	response := ut.PerformRequest(
		h.Engine, "PUT", "/api/security-settings",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
	).Result()
	if response.StatusCode() != consts.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if service.updateCall {
		t.Fatal("UpdateSecurity() called after checkpoint failure")
	}
}

func TestUpdateSecuritySettingsRejectsUnknownField(t *testing.T) {
	service := &fakeSecuritySettingsService{
		view: &config.SecuritySettingsView{Settings: config.SecuritySettings{P2PScanEnabled: true}},
	}
	h := server.New()
	h.PUT("/api/security-settings", UpdateSecuritySettings(service, &fakeP2PCheckpointAdvancer{}))
	body := []byte(`{"p2p_scan_enabled":true,"unknown":true}`)

	response := ut.PerformRequest(
		h.Engine, "PUT", "/api/security-settings",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
	).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestGetSecuritySettingsReportsL4Capability(t *testing.T) {
	service := &fakeSecuritySettingsService{view: &config.SecuritySettingsView{
		Settings: config.SecuritySettings{P2PScanEnabled: true},
		L4DocumentRead: config.SecurityCapability{
			Enforceable: false,
			Enabled:     false,
			Message:     "not enforceable",
		},
	}}
	h := server.New()
	h.GET("/api/security-settings", GetSecuritySettings(service))

	response := ut.PerformRequest(h.Engine, "GET", "/api/security-settings", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data config.SecuritySettingsView `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.L4DocumentRead.Enforceable || payload.Data.L4DocumentRead.Enabled {
		t.Fatalf("l4 capability = %#v", payload.Data.L4DocumentRead)
	}
}
