package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/chat"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

func GetChatHistory(svc *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		threadID := c.Param("thread_id")
		if threadID == "" {
			threadID = c.Query("thread_id")
		}
		history, err := svc.History(threadID)
		if err != nil {
			switch {
			case errors.Is(err, chat.ErrInvalidThreadID):
				writeAPIError(c, 400, 40061, err)
			case errors.Is(err, chat.ErrHistoryNotFound):
				writeAPIError(c, 404, 40461, err)
			default:
				writeAPIError(c, 500, 50061, err)
			}
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": history})
	}
}

type chatPageContext struct {
	ActiveKey string             `json:"active_key"`
	Selection *chatPageSelection `json:"selection"`
	ViewState json.RawMessage    `json:"view_state"`
}

type chatPageSelection struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id"`
	Label string `json:"label"`
}

// Chat 是流式对话 SSE handler。它同步阻塞地边读 codex 边写 SSE——绝不起后台
// goroutine 后立即 return（那样连接会被 Hertz 关掉）。事件类型：
//
//	thread  data={"thread_id":"..."}
//	delta   data={"text":"..."}
//	done    data={}
//	error   data={"message":"..."}
//
// 一旦进入 SSE（响应头已发），出错只能通过 error 事件传达，不能再改 HTTP 状态码。
func Chat(svc *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		ctx = observability.FromRequestContext(ctx, c)
		req, cleanup, err := decodeChatMultipart(c)
		if err != nil {
			// 尚未进入 SSE，正常返回 HTTP 400。
			writeAPIError(c, 400, 40060, err)
			return
		}
		defer cleanup()

		w := newSSEWriter(c)
		defer func() {
			if err := w.Close(); err != nil {
				hlog.CtxErrorf(ctx, "close chat stream failed error=%+v", err)
			}
		}()

		emit := func(ev chat.Event) error {
			switch ev.Kind {
			case chat.EventThread:
				data, err := json.Marshal(map[string]string{"thread_id": ev.ThreadID})
				if err != nil {
					return fmt.Errorf("marshal thread event: %w", err)
				}
				return w.WriteEvent("thread", data)
			case chat.EventDelta:
				data, err := json.Marshal(map[string]string{"text": ev.Text})
				if err != nil {
					return fmt.Errorf("marshal delta event: %w", err)
				}
				return w.WriteEvent("delta", data)
			default:
				return fmt.Errorf("unknown chat event kind %q", ev.Kind)
			}
		}

		if err := svc.Stream(ctx, req, emit); err != nil {
			// fail-fast：把错误作为 error 事件发出（此时响应头已发，无法再改状态码）。
			hlog.CtxErrorf(ctx, "chat stream failed error=%+v", err)
			data, marshalErr := json.Marshal(map[string]string{"message": err.Error()})
			if marshalErr != nil {
				hlog.CtxErrorf(ctx, "marshal chat error event failed original_error=%+v marshal_error=%+v", err, marshalErr)
				data = []byte(`{"message":"chat failed"}`)
			}
			if writeErr := w.WriteEvent("error", data); writeErr != nil {
				hlog.CtxErrorf(ctx, "write chat error event failed original_error=%+v write_error=%+v", err, writeErr)
			}
			return
		}
		// 本轮正常结束：发 done。data 必须非空（SSE writer 会忽略零长 data），发 "{}"。
		if err := w.WriteEvent("done", []byte("{}")); err != nil {
			hlog.CtxErrorf(ctx, "write chat done event failed error=%+v", err)
		}
	}
}

func decodeChatMultipart(c *app.RequestContext) (req chat.Request, cleanup func(), err error) {
	form, err := c.MultipartForm()
	if err != nil {
		return chat.Request{}, nil, fmt.Errorf("chat request must be multipart/form-data: %w", err)
	}
	cleanup = func() { c.Request.RemoveMultipartFormFiles() }
	fail := func(cause error) (chat.Request, func(), error) {
		cleanup()
		return chat.Request{}, nil, cause
	}

	allowedValues := map[string]bool{"message": true, "thread_id": true, "page_context": true, "user_open_id": true}
	for key := range form.Value {
		if !allowedValues[key] {
			return fail(fmt.Errorf("unknown chat form field %q", key))
		}
	}
	for key := range form.File {
		if key != "image" {
			return fail(fmt.Errorf("unknown chat file field %q", key))
		}
	}

	messages := form.Value["message"]
	if len(messages) != 1 || strings.TrimSpace(messages[0]) == "" {
		return fail(fmt.Errorf("chat message is required exactly once"))
	}
	req.Message = messages[0]

	if values := form.Value["thread_id"]; len(values) > 1 {
		return fail(fmt.Errorf("chat thread_id must appear at most once"))
	} else if len(values) == 1 {
		req.ThreadID = values[0]
	}

	if values := form.Value["user_open_id"]; len(values) > 1 {
		return fail(fmt.Errorf("chat user_open_id must appear at most once"))
	} else if len(values) == 1 {
		req.UserOpenID = strings.TrimSpace(values[0])
	}

	if values := form.Value["page_context"]; len(values) > 1 {
		return fail(fmt.Errorf("chat page_context must appear at most once"))
	} else if len(values) == 1 {
		var body *chatPageContext
		if err := decodeStrictJSON([]byte(values[0]), &body); err != nil {
			return fail(fmt.Errorf("decode chat page_context: %w", err))
		}
		if body != nil {
			pc := &chat.PageContext{ActiveKey: body.ActiveKey, ViewState: body.ViewState}
			if sel := body.Selection; sel != nil {
				pc.Selection = &chat.PageSelection{Kind: sel.Kind, ID: sel.ID, Label: sel.Label}
			}
			req.PageContext = pc
		}
	}

	images := form.File["image"]
	if len(images) > 1 {
		return fail(fmt.Errorf("chat image must appear at most once"))
	}
	if len(images) == 1 {
		file, err := images[0].Open()
		if err != nil {
			return fail(fmt.Errorf("open chat image: %w", err))
		}
		path, removeImage, saveErr := chat.SaveTemporaryImage(file)
		closeErr := file.Close()
		if saveErr != nil {
			return fail(saveErr)
		}
		if closeErr != nil {
			removeImage()
			return fail(fmt.Errorf("close chat image upload: %w", closeErr))
		}
		req.ImagePath = path
		removeMultipart := cleanup
		cleanup = func() {
			removeImage()
			removeMultipart()
		}
	}
	return req, cleanup, nil
}
