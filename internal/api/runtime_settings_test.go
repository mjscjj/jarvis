package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"jarvis/internal/config"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeRuntimeSettingsService struct {
	view      *config.RuntimeSettingsView
	update    config.RuntimeSettings
	updateErr error
}

func (f *fakeRuntimeSettingsService) Get(context.Context) (*config.RuntimeSettingsView, error) {
	return f.view, nil
}

func (f *fakeRuntimeSettingsService) Update(_ context.Context, input config.RuntimeSettings) (*config.RuntimeSettingsView, error) {
	f.update = input
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.view, nil
}

func TestGetRuntimeSettings(t *testing.T) {
	service := &fakeRuntimeSettingsService{view: &config.RuntimeSettingsView{
		Settings: config.RuntimeSettings{AnalysisCLI: "traex", ExecuteCLI: "codex"},
	}}
	h := server.New()
	h.GET("/api/runtime-settings", GetRuntimeSettings(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/runtime-settings", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Code int                        `json:"code"`
		Data config.RuntimeSettingsView `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.Settings.AnalysisCLI != "traex" || payload.Data.Settings.ExecuteCLI != "codex" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestUpdateRuntimeSettings(t *testing.T) {
	service := &fakeRuntimeSettingsService{view: &config.RuntimeSettingsView{}}
	h := server.New()
	h.PUT("/api/runtime-settings", UpdateRuntimeSettings(service))
	body := []byte(`{"analysis_cli":"codex","analysis_model":"gpt","analysis_timeout_seconds":600,"extract_enabled":true,"extract_engine":"codex","extract_reasoning_effort":"low","execute_auto_enabled":true,"execute_cli":"traex","execute_model":"exec","execute_reasoning_effort":"high","execute_timeout_seconds":1800,"execute_stale_minutes":45,"execute_concurrency":3,"chat_enabled":true,"chat_model":"chat","chat_reasoning_effort":"medium","chat_timeout_seconds":600,"scheduled_task_enabled":true,"daily_digest_enabled":true}`)
	response := ut.PerformRequest(h.Engine, "PUT", "/api/runtime-settings", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if service.update.AnalysisCLI != "codex" || service.update.ExecuteCLI != "traex" || service.update.ExecuteReasoningEffort != "high" {
		t.Fatalf("update = %#v", service.update)
	}
}

func TestUpdateRuntimeSettingsRejectsUnknownField(t *testing.T) {
	service := &fakeRuntimeSettingsService{}
	h := server.New()
	h.PUT("/api/runtime-settings", UpdateRuntimeSettings(service))
	body := []byte(`{"analysis_cli":"codex","unknown":true}`)
	response := ut.PerformRequest(h.Engine, "PUT", "/api/runtime-settings", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestUpdateRuntimeSettingsRejectsRetiredDecideSetting(t *testing.T) {
	service := &fakeRuntimeSettingsService{}
	h := server.New()
	h.PUT("/api/runtime-settings", UpdateRuntimeSettings(service))
	body := []byte(`{"decide_enabled":true}`)
	response := ut.PerformRequest(h.Engine, "PUT", "/api/runtime-settings", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestUpdateRuntimeSettingsMapsValidationError(t *testing.T) {
	service := &fakeRuntimeSettingsService{updateErr: fmt.Errorf("%w: fixture", config.ErrInvalidRuntimeSettings)}
	h := server.New()
	h.PUT("/api/runtime-settings", UpdateRuntimeSettings(service))
	body := []byte(`{}`)
	response := ut.PerformRequest(h.Engine, "PUT", "/api/runtime-settings", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}
