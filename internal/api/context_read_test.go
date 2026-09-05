package api

import (
	"context"
	"encoding/json"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"jarvis/internal/contextpack"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"strings"
	"testing"
)

type contextTaskService struct {
	fakeTaskService
	content json.RawMessage
}

func (s *contextTaskService) GetTask(_ context.Context, id uint64) (*execute.TaskView, error) {
	return &execute.TaskView{ID: id, Version: 4, SourcePayload: s.content}, nil
}

type contextTodoReader struct {
	fakeTodoReader
	content json.RawMessage
}

func (s *contextTodoReader) GetTodo(_ context.Context, id uint64) (*extract.TodoView, error) {
	return &extract.TodoView{ID: id, Version: 4, Revision: 3, Content: s.content}, nil
}

func TestFrozenContextReadAPI(t *testing.T) {
	raw, err := contextpack.Freeze([]byte(`{"request":"原始请求"}`), []byte(`{"messages":[{"message_id":"om_1","content":"冻结消息正文"}],"project":{"summary":"完整背景正文"}}`), "概览", nil)
	if err != nil {
		t.Fatal(err)
	}
	h := server.Default()
	h.GET("/api/tasks/:task_id", GetTask(&contextTaskService{content: raw}))
	h.GET("/api/todos/:todo_id", GetTodo(&contextTodoReader{content: raw}))
	for _, test := range []struct {
		path         string
		status       int
		want, absent string
	}{
		{"/api/tasks/9", 200, "原始请求", "完整背景正文"},
		{"/api/tasks/9?context=background", 200, "完整背景正文", "冻结消息正文"},
		{"/api/tasks/9?context=project", 200, "完整背景正文", "原始请求"},
		{"/api/tasks/9?context=conversation", 200, "冻结消息正文", "完整背景正文"},
		{"/api/tasks/9?message_id=om_1", 200, "冻结消息正文", "原始请求"},
		{"/api/tasks/9?context=full", 200, "完整背景正文", ""},
		{"/api/tasks/9?context=conversation&message_id=om_1", 400, "mutually exclusive", ""},
		{"/api/tasks/9?message_id=unknown", 400, "unknown frozen message_id", ""},
		{"/api/todos/7?revision=3&context=project", 200, "完整背景正文", ""},
		{"/api/todos/7?revision=2&context=project", 409, "revision changed", "完整背景正文"},
	} {
		t.Run(test.path, func(t *testing.T) {
			r := ut.PerformRequest(h.Engine, "GET", test.path, nil).Result()
			body := string(r.Body())
			if r.StatusCode() != test.status || !strings.Contains(body, test.want) || (test.absent != "" && strings.Contains(body, test.absent)) {
				t.Fatalf("HTTP %d: %s", r.StatusCode(), body)
			}
		})
	}
}
