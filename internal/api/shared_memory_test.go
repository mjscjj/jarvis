package api

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"jarvis/internal/sharedmem"

	"code.byted.org/middleware/hertz/pkg/app/server"
	"code.byted.org/middleware/hertz/pkg/common/ut"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
)

type fakeSharedMemoryService struct {
	view          *sharedmem.SharedMemoryView
	upsertContent string
	upsertBy      string
}

func (f *fakeSharedMemoryService) Get(_ context.Context) (*sharedmem.SharedMemoryView, error) {
	return f.view, nil
}

func (f *fakeSharedMemoryService) Upsert(_ context.Context, content, updatedBy string) (*sharedmem.SharedMemoryView, error) {
	f.upsertContent = content
	f.upsertBy = updatedBy
	return &sharedmem.SharedMemoryView{Content: content, UpdatedBy: updatedBy, Saved: true}, nil
}

func TestGetSharedMemory(t *testing.T) {
	svc := &fakeSharedMemoryService{view: &sharedmem.SharedMemoryView{Content: "hello", UpdatedBy: "agent", Saved: true}}
	h := server.New()
	h.GET("/api/shared-memory", GetSharedMemory(svc))
	response := ut.PerformRequest(h.Engine, "GET", "/api/shared-memory", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Code int                        `json:"code"`
		Data sharedmem.SharedMemoryView `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Code != 0 || payload.Data.Content != "hello" || payload.Data.UpdatedBy != "agent" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestUpdateSharedMemorySaves(t *testing.T) {
	svc := &fakeSharedMemoryService{}
	h := server.New()
	h.PUT("/api/shared-memory", UpdateSharedMemory(svc))
	body := []byte(`{"content":"new memory"}`)
	response := ut.PerformRequest(h.Engine, "PUT", "/api/shared-memory", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if svc.upsertContent != "new memory" || svc.upsertBy != sharedMemoryUpdatedBy {
		t.Fatalf("upsert content=%q by=%q", svc.upsertContent, svc.upsertBy)
	}
}

func TestUpdateSharedMemoryAllowsEmptyContent(t *testing.T) {
	svc := &fakeSharedMemoryService{}
	h := server.New()
	h.PUT("/api/shared-memory", UpdateSharedMemory(svc))
	body := []byte(`{"content":""}`)
	response := ut.PerformRequest(h.Engine, "PUT", "/api/shared-memory", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestUpdateSharedMemoryRejectsInvalidBody(t *testing.T) {
	svc := &fakeSharedMemoryService{}
	h := server.New()
	h.PUT("/api/shared-memory", UpdateSharedMemory(svc))
	for _, body := range [][]byte{
		[]byte(`not json`),
		[]byte(`{"content":"x","extra":true}`),
	} {
		response := ut.PerformRequest(h.Engine, "PUT", "/api/shared-memory", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
		if response.StatusCode() != consts.StatusBadRequest {
			t.Fatalf("body=%s status=%d resp=%s", body, response.StatusCode(), response.Body())
		}
	}
}
