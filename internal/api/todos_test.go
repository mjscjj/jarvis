package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"jarvis/internal/extract"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
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

	recorder := ut.PerformRequest(h.Engine, "GET", "/api/todos?status=extracted,need_info&leader_only=true&page=2&page_size=10", nil)
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
	h.GET("/api/todos", ListTodos(&fakeTodoReader{}))
	response := ut.PerformRequest(h.Engine, "GET", "/api/todos?leader_only=maybe", nil).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
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
