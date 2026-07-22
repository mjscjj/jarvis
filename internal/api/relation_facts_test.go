package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"jarvis/internal/knowledge"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeFactService struct {
	createInput  knowledge.CreateInput
	listFilter   knowledge.FactFilter
	retractInput knowledge.RetractInput
	err          error
}

func (f *fakeFactService) Create(_ context.Context, input knowledge.CreateInput) (*knowledge.FactView, error) {
	f.createInput = input
	return &knowledge.FactView{ID: 9, Subject: input.Subject, Predicate: input.Predicate}, f.err
}

func (f *fakeFactService) List(_ context.Context, filter knowledge.FactFilter) (*knowledge.FactList, error) {
	f.listFilter = filter
	return &knowledge.FactList{Page: filter.Page, PageSize: filter.PageSize}, f.err
}

func (f *fakeFactService) Retract(_ context.Context, input knowledge.RetractInput) (*knowledge.FactView, error) {
	f.retractInput = input
	return &knowledge.FactView{ID: input.FactID, Status: "retracted"}, f.err
}

func TestCreateRelationFact(t *testing.T) {
	svc := &fakeFactService{}
	h := server.New()
	h.POST("/api/relation-facts", CreateRelationFact(svc))
	body := []byte(`{"subject":{"type":"project","id":3},"predicate":"uses","value":{"name":"MySQL"},"assertion_kind":"manual","source_type":"user","source_id":"request-1"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/relation-facts", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if svc.createInput.Subject.ID != 3 || svc.createInput.Predicate != "uses" || string(svc.createInput.Value) != `{"name":"MySQL"}` {
		t.Fatalf("create input = %#v", svc.createInput)
	}
}

func TestCreateRelationFactRejectsUnknownField(t *testing.T) {
	h := server.New()
	h.POST("/api/relation-facts", CreateRelationFact(&fakeFactService{}))
	body := []byte(`{"subject":{"type":"project","id":3},"predicate":"uses","unknown":true}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/relation-facts", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestListRelationFactsParsesTemporalFilter(t *testing.T) {
	svc := &fakeFactService{}
	h := server.New()
	h.GET("/api/relation-facts", ListRelationFacts(svc))
	response := ut.PerformRequest(h.Engine, "GET", "/api/relation-facts?subject_type=project&subject_id=3&predicate=depends_on&include_inactive=true&as_of=2026-07-22T08%3A00%3A00%2B08%3A00&page=2&page_size=10", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if svc.listFilter.SubjectType == nil || *svc.listFilter.SubjectType != knowledge.EntityProject || svc.listFilter.SubjectID == nil || *svc.listFilter.SubjectID != 3 {
		t.Fatalf("subject filter = %#v", svc.listFilter)
	}
	if !svc.listFilter.IncludeInactive || svc.listFilter.AsOf.IsZero() || svc.listFilter.Page != 2 || svc.listFilter.PageSize != 10 {
		t.Fatalf("list filter = %#v", svc.listFilter)
	}
}

func TestRetractRelationFactMapsConflict(t *testing.T) {
	svc := &fakeFactService{err: knowledge.ErrNotActive}
	h := server.New()
	h.POST("/api/relation-facts/:fact_id/retract", RetractRelationFact(svc))
	body := []byte(`{"by":"user","reason":"incorrect"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/relation-facts/12/retract", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusConflict {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if svc.retractInput.FactID != 12 || svc.retractInput.By != "user" {
		t.Fatalf("retract input = %#v", svc.retractInput)
	}
	var payload struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 40960 {
		t.Fatalf("code = %d, want 40960", payload.Code)
	}
}

func TestListRelationFactsMapsInvalidInput(t *testing.T) {
	svc := &fakeFactService{err: errors.New("database failed")}
	h := server.New()
	h.GET("/api/relation-facts", ListRelationFacts(svc))
	response := ut.PerformRequest(h.Engine, "GET", "/api/relation-facts?subject_type=project", nil).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}
