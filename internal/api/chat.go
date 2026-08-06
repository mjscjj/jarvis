package api

import (
	"context"
	"encoding/json"
	"fmt"

	"jarvis/internal/chat"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

// chatRequestBody 是 POST /api/chat 的请求体，字段严格对齐前端冻结契约
// （web/src/types.ts 的 ChatRequest）。用指针区分「字段缺失」与「显式 null」。
type chatRequestBody struct {
	Message     string           `json:"message"`
	ThreadID    *string          `json:"thread_id"`
	PageContext *chatPageContext `json:"page_context"`
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
		var body chatRequestBody
		if err := decodeStrictJSON(c.Request.Body(), &body); err != nil {
			// 尚未进入 SSE，正常返回 HTTP 400。
			writeAPIError(c, 400, 40060, err)
			return
		}

		w := newSSEWriter(c)
		defer func() {
			if err := w.Close(); err != nil {
				hlog.CtxErrorf(ctx, "close chat stream failed error=%+v", err)
			}
		}()

		req := chat.Request{Message: body.Message}
		if body.ThreadID != nil {
			req.ThreadID = *body.ThreadID
		}
		if body.PageContext != nil {
			pc := &chat.PageContext{ActiveKey: body.PageContext.ActiveKey, ViewState: body.PageContext.ViewState}
			if sel := body.PageContext.Selection; sel != nil {
				pc.Selection = &chat.PageSelection{Kind: sel.Kind, ID: sel.ID, Label: sel.Label}
			}
			req.PageContext = pc
		}

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
