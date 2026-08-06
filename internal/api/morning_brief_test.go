package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"jarvis/internal/morningbrief"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeMorningBriefService struct {
	limit   int
	items   []morningbrief.Brief
	listErr error
}

func (f *fakeMorningBriefService) List(_ context.Context, limit int) ([]morningbrief.Brief, error) {
	f.limit = limit
	return f.items, f.listErr
}

func TestListMorningBriefsReturnsItems(t *testing.T) {
	generatedAt := time.Date(2026, 8, 4, 8, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	service := &fakeMorningBriefService{items: []morningbrief.Brief{{
		Date: "2026-08-04", Content: "# 晨间作战简报", GeneratedAt: generatedAt,
	}}}
	h := server.New()
	h.GET("/api/morning-briefs", ListMorningBriefs(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/morning-briefs?limit=7", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if service.limit != 7 {
		t.Fatalf("limit = %d", service.limit)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Items []morningbrief.Brief `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Code != 0 || len(payload.Data.Items) != 1 || payload.Data.Items[0].Date != "2026-08-04" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestListMorningBriefsUsesDefaultLimit(t *testing.T) {
	service := &fakeMorningBriefService{items: []morningbrief.Brief{}}
	h := server.New()
	h.GET("/api/morning-briefs", ListMorningBriefs(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/morning-briefs", nil).Result()
	if response.StatusCode() != consts.StatusOK || service.limit != defaultMorningBriefLimit {
		t.Fatalf("status=%d limit=%d body=%s", response.StatusCode(), service.limit, response.Body())
	}
}

func TestListMorningBriefsRejectsInvalidLimit(t *testing.T) {
	for _, limit := range []string{"0", "32", "nope"} {
		t.Run(limit, func(t *testing.T) {
			service := &fakeMorningBriefService{}
			h := server.New()
			h.GET("/api/morning-briefs", ListMorningBriefs(service))
			response := ut.PerformRequest(h.Engine, "GET", "/api/morning-briefs?limit="+limit, nil).Result()
			if response.StatusCode() != consts.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
			}
		})
	}
}

func TestListMorningBriefsMapsReaderFailure(t *testing.T) {
	service := &fakeMorningBriefService{listErr: errors.New("artifact incomplete")}
	h := server.New()
	h.GET("/api/morning-briefs", ListMorningBriefs(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/morning-briefs", nil).Result()
	if response.StatusCode() != consts.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}
