package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"jarvis/internal/dailydigest"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeDailyDigestService struct {
	listDate    string
	listResult  []dailydigest.DigestView
	listErr     error
	kickScope   string
	kickScopeID string
	kickDate    string
	kickErr     error
}

func (f *fakeDailyDigestService) ListByDate(_ context.Context, date string) ([]dailydigest.DigestView, error) {
	f.listDate = date
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeDailyDigestService) KickGenerateOne(_ context.Context, scope, scopeID, date string) (*dailydigest.KickResult, error) {
	f.kickScope, f.kickScopeID, f.kickDate = scope, scopeID, date
	if f.kickErr != nil {
		return nil, f.kickErr
	}
	return &dailydigest.KickResult{Scope: scope, ScopeID: scopeID, Date: date, Status: dailydigest.StatusGenerating}, nil
}

func TestGetDailyDigestsReturnsItems(t *testing.T) {
	service := &fakeDailyDigestService{listResult: []dailydigest.DigestView{
		{ID: 1, Scope: "person", ScopeID: "ou_x", DigestDate: "2026-07-22", Status: "done", Engine: "codex"},
		{ID: 2, Scope: "group", ScopeID: "9", DigestDate: "2026-07-22", Status: "done", Engine: "codex"},
	}}
	h := server.New()
	h.GET("/api/daily-digests", GetDailyDigests(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/daily-digests?date=2026-07-22", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if service.listDate != "2026-07-22" {
		t.Fatalf("listDate = %q", service.listDate)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Items []dailydigest.DigestView `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Code != 0 || len(payload.Data.Items) != 2 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestGetDailyDigestsRejectsBadDate(t *testing.T) {
	service := &fakeDailyDigestService{listErr: fmt.Errorf("%w: digest date \"nope\" must be YYYY-MM-DD", dailydigest.ErrInvalidInput)}
	h := server.New()
	h.GET("/api/daily-digests", GetDailyDigests(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/daily-digests?date=nope", nil).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestGenerateDailyDigestKicks(t *testing.T) {
	service := &fakeDailyDigestService{}
	h := server.New()
	h.POST("/api/daily-digests/generate", GenerateDailyDigest(service))
	body := []byte(`{"scope":"group","scope_id":"9","date":"2026-07-22"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/daily-digests/generate", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if service.kickScope != "group" || service.kickScopeID != "9" || service.kickDate != "2026-07-22" {
		t.Fatalf("kick args = %q %q %q", service.kickScope, service.kickScopeID, service.kickDate)
	}
	var payload struct {
		Code int                    `json:"code"`
		Data dailydigest.KickResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Data.Status != dailydigest.StatusGenerating {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestGenerateDailyDigestRequiresScope(t *testing.T) {
	service := &fakeDailyDigestService{}
	h := server.New()
	h.POST("/api/daily-digests/generate", GenerateDailyDigest(service))
	body := []byte(`{"scope":"","scope_id":"9"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/daily-digests/generate", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestGenerateDailyDigestMapsInvalidInput(t *testing.T) {
	service := &fakeDailyDigestService{kickErr: fmt.Errorf("%w: group scope_id \"9\" is not a key group", dailydigest.ErrInvalidInput)}
	h := server.New()
	h.POST("/api/daily-digests/generate", GenerateDailyDigest(service))
	body := []byte(`{"scope":"group","scope_id":"9"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/daily-digests/generate", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestGenerateDailyDigestMapsAlreadyGeneratingToConflict(t *testing.T) {
	service := &fakeDailyDigestService{kickErr: fmt.Errorf("%w: person digest", dailydigest.ErrAlreadyGenerating)}
	h := server.New()
	h.POST("/api/daily-digests/generate", GenerateDailyDigest(service))
	body := []byte(`{"scope":"person","scope_id":"ou_me","date":"2026-07-22"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/daily-digests/generate", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusConflict {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}
