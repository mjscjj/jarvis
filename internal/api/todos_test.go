package api

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	"jarvis/internal/extract"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"jarvis/internal/observability"
)

type fakeTodoReader struct {
	filter extract.TodoListFilter
}

func (f *fakeTodoReader) ListTodos(_ context.Context, filter extract.TodoListFilter) (*extract.TodoList, error) {
	f.filter = filter
	return &extract.TodoList{Items: []extract.TodoView{{ID: 7, Title: "fixture"}}, Total: 1, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (f *fakeTodoReader) GetTodo(_ context.Context, id uint64) (*extract.TodoView, error) {
	if id != 7 {
		return nil, fmt.Errorf("%w: id=%d", extract.ErrTodoNotFound, id)
	}
	return &extract.TodoView{ID: id, Title: "fixture"}, nil
}

func TestListTodos(t *testing.T) {
	reader := &fakeTodoReader{}
	h := server.New()
	h.GET("/api/todos", ListTodos(reader))

	recorder := ut.PerformRequest(h.Engine, "GET", "/api/todos?status=extracted,observing&leader_only=true&page=2&page_size=10", nil)
	response := recorder.Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if reader.filter.Page != 2 || reader.filter.PageSize != 10 || reader.filter.LeaderOnly == nil || !*reader.filter.LeaderOnly {
		t.Fatalf("filter = %#v", reader.filter)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 0 || payload.Data.Total != 1 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestListTodosRejectsInvalidQuery(t *testing.T) {
	h := server.New()
	h.Use(observability.Middleware())
	h.GET("/api/todos", ListTodos(&fakeTodoReader{}))
	response := ut.PerformRequest(h.Engine, "GET", "/api/todos?leader_only=maybe", nil).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	logID := string(response.Header.Peek(observability.HeaderLogID))
	if !regexp.MustCompile(`^\d{13}[0-9a-f]{16}$`).MatchString(logID) {
		t.Fatalf("response LogID = %q, want a generated LogID", logID)
	}
	var payload struct {
		LogID string `json:"logid"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.LogID != logID {
		t.Fatalf("body LogID = %q, header LogID = %q", payload.LogID, logID)
	}
}

func TestHertzReusesInboundLogID(t *testing.T) {
	h := server.New()
	h.Use(observability.Middleware())
	h.GET("/api/todos", ListTodos(&fakeTodoReader{}))

	const inbound = "02-inbound-test-logid"
	response := ut.PerformRequest(
		h.Engine,
		"GET",
		"/api/todos?leader_only=maybe",
		nil,
		ut.Header{Key: observability.HeaderLogID, Value: inbound},
	).Result()
	if got := string(response.Header.Peek(observability.HeaderLogID)); got != inbound {
		t.Fatalf("response LogID = %q, want %q", got, inbound)
	}
}

func TestListTodosRejectsUnknownStatus(t *testing.T) {
	h := server.New()
	h.GET("/api/todos", ListTodos(&fakeTodoReader{}))
	response := ut.PerformRequest(h.Engine, "GET", "/api/todos?status=duplicate", nil).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}

func TestGetTodoNotFound(t *testing.T) {
	h := server.New()
	h.GET("/api/todos/:todo_id", GetTodo(&fakeTodoReader{}))
	response := ut.PerformRequest(h.Engine, "GET", "/api/todos/9", nil).Result()
	if response.StatusCode() != consts.StatusNotFound {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
}
