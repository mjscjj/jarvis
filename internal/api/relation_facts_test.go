package api

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"jarvis/internal/knowledge"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeFactService struct {
	createInput knowledge.CreateInput
	updateInput knowledge.UpdateInput
	listFilter  knowledge.FactFilter
	deletedID   uint64
	err         error
}

func (f *fakeFactService) Create(_ context.Context, input knowledge.CreateInput) (*knowledge.FactView, error) {
	f.createInput = input
	return &knowledge.FactView{ID: 9, EntityA: input.EntityA, EntityB: input.EntityB, Description: input.Description}, f.err
}

func (f *fakeFactService) List(_ context.Context, filter knowledge.FactFilter) (*knowledge.FactList, error) {
	f.listFilter = filter
	return &knowledge.FactList{Page: filter.Page, PageSize: filter.PageSize}, f.err
}

func (f *fakeFactService) Update(_ context.Context, input knowledge.UpdateInput) (*knowledge.FactView, error) {
	f.updateInput = input
	return &knowledge.FactView{ID: input.FactID, Description: input.Description}, f.err
}

func (f *fakeFactService) Delete(_ context.Context, factID uint64) error {
	f.deletedID = factID
	return f.err
}

func TestCreateRelationFact(t *testing.T) {
	svc := &fakeFactService{}
	h := server.New()
	h.POST("/api/relation-facts", CreateRelationFact(svc))
	body := []byte(`{"entity_a":{"type":"person","id":7},"entity_b":{"type":"project","id":3},"description":"张三负责这个项目。"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/relation-facts", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if svc.createInput.EntityA.ID != 7 || svc.createInput.EntityB.ID != 3 || svc.createInput.Description == "" {
		t.Fatalf("create input = %#v", svc.createInput)
	}
}

func TestCreateRelationFactRejectsUnknownField(t *testing.T) {
	h := server.New()
	h.POST("/api/relation-facts", CreateRelationFact(&fakeFactService{}))
	body := []byte(`{"entity_a":{"type":"person","id":7},"entity_b":{"type":"project","id":3},"description":"有关联","unknown":true}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/relation-facts", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestListRelationFactsParsesEntityFilter(t *testing.T) {
	svc := &fakeFactService{}
	h := server.New()
	h.GET("/api/relation-facts", ListRelationFacts(svc))
	response := ut.PerformRequest(h.Engine, "GET", "/api/relation-facts?entity_type=project&entity_id=3&page=2&page_size=10", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if svc.listFilter.EntityType == nil || *svc.listFilter.EntityType != knowledge.EntityProject || svc.listFilter.EntityID == nil || *svc.listFilter.EntityID != 3 {
		t.Fatalf("entity filter = %#v", svc.listFilter)
	}
	if svc.listFilter.Page != 2 || svc.listFilter.PageSize != 10 {
		t.Fatalf("list filter = %#v", svc.listFilter)
	}
}

func TestUpdateAndDeleteRelationFact(t *testing.T) {
	svc := &fakeFactService{}
	h := server.New()
	h.PUT("/api/relation-facts/:fact_id", UpdateRelationFact(svc))
	h.DELETE("/api/relation-facts/:fact_id", DeleteRelationFact(svc))
	body := []byte(`{"description":"关系已更新。"}`)
	response := ut.PerformRequest(h.Engine, "PUT", "/api/relation-facts/12", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK || svc.updateInput.FactID != 12 {
		t.Fatalf("update status=%d input=%#v body=%s", response.StatusCode(), svc.updateInput, response.Body())
	}
	response = ut.PerformRequest(h.Engine, "DELETE", "/api/relation-facts/12", nil).Result()
	if response.StatusCode() != consts.StatusOK || svc.deletedID != 12 {
		t.Fatalf("delete status=%d id=%d body=%s", response.StatusCode(), svc.deletedID, response.Body())
	}
}

func TestListRelationFactsMapsInvalidInput(t *testing.T) {
	svc := &fakeFactService{err: errors.New("database failed")}
	h := server.New()
	h.GET("/api/relation-facts", ListRelationFacts(svc))
	response := ut.PerformRequest(h.Engine, "GET", "/api/relation-facts?entity_type=project", nil).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}
