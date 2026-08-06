package api

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"jarvis/internal/sharedmem"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeSharedMemoryService struct {
	view          *sharedmem.SharedMemoryView
	upsertContent string
	appendNote    string
}

func (f *fakeSharedMemoryService) Get(_ context.Context) (*sharedmem.SharedMemoryView, error) {
	return f.view, nil
}

func (f *fakeSharedMemoryService) Upsert(_ context.Context, content string) (*sharedmem.SharedMemoryView, error) {
	f.upsertContent = content
	return &sharedmem.SharedMemoryView{Content: content, Path: "/tmp/shared-memory.md", Saved: true}, nil
}

func (f *fakeSharedMemoryService) Append(_ context.Context, note string) (*sharedmem.SharedMemoryView, error) {
	f.appendNote = note
	return &sharedmem.SharedMemoryView{Content: note, Path: "/tmp/shared-memory.md", Saved: true}, nil
}

func TestGetSharedMemory(t *testing.T) {
	svc := &fakeSharedMemoryService{view: &sharedmem.SharedMemoryView{Content: "hello", Path: "/tmp/shared-memory.md", Saved: true}}
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
	if payload.Code != 0 || payload.Data.Content != "hello" || payload.Data.Path != "/tmp/shared-memory.md" {
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
	if svc.upsertContent != "new memory" {
		t.Fatalf("upsert content=%q", svc.upsertContent)
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

func TestAppendSharedMemoryAppendsOneNote(t *testing.T) {
	svc := &fakeSharedMemoryService{}
	h := server.New()
	h.POST("/api/shared-memory/append", AppendSharedMemory(svc))
	body := []byte(`{"note":"new fact"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/shared-memory/append", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if svc.appendNote != "new fact" {
		t.Fatalf("append note=%q", svc.appendNote)
	}
}

func TestAppendSharedMemoryRejectsInvalidBody(t *testing.T) {
	svc := &fakeSharedMemoryService{}
	h := server.New()
	h.POST("/api/shared-memory/append", AppendSharedMemory(svc))
	for _, body := range [][]byte{
		[]byte(`{"note":"x","extra":true}`),
		[]byte(`{"note":`),
	} {
		response := ut.PerformRequest(h.Engine, "POST", "/api/shared-memory/append", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
		if response.StatusCode() != consts.StatusBadRequest {
			t.Fatalf("body=%s status=%d resp=%s", body, response.StatusCode(), response.Body())
		}
	}
}

func TestAppendSharedMemoryRejectsBlankNote(t *testing.T) {
	svc := &fakeSharedMemoryService{}
	h := server.New()
	h.POST("/api/shared-memory/append", AppendSharedMemory(svc))
	body := []byte(`{"note":"  "}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/shared-memory/append", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status=%d resp=%s", response.StatusCode(), response.Body())
	}
	if svc.appendNote != "" {
		t.Fatalf("append unexpectedly called with %q", svc.appendNote)
	}
}
