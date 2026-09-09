package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/chat"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

const defaultChatThreadListLimit = 50

func ListChatThreads(svc *chat.Service) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		limit := defaultChatThreadListLimit
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 100 {
				writeAPIError(c, 400, 40062, fmt.Errorf("limit must be an integer between 1 and 100"))
				return
			}
			limit = parsed
		}
		threads, err := svc.Threads(limit)
		if err != nil {
			writeAPIError(c, 500, 50062, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": threads})
	}
}

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

// GetChat serves both history detail and the thread index on the stable
// /api/chat path. Production only needs to forward this one endpoint; the
// /api/chat/threads alias remains available for direct sidecar clients.
func GetChat(svc *chat.Service) app.HandlerFunc {
	history := GetChatHistory(svc)
	threads := ListChatThreads(svc)
	return func(ctx context.Context, c *app.RequestContext) {
		switch strings.TrimSpace(c.Query("view")) {
		case "":
			history(ctx, c)
		case "threads":
			threads(ctx, c)
		default:
			writeAPIError(c, 400, 40063, fmt.Errorf("unknown chat view %q", c.Query("view")))
		}
	}
}

func StopChatTurn(svc *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		turnID := strings.TrimSpace(c.Query("turn_id"))
		err := svc.StopTurn(ctx, turnID)
		if err != nil {
			if errors.Is(err, chat.ErrInvalidTurnID) {
				writeAPIError(c, 400, 40064, err)
			} else {
				writeAPIError(c, 500, 50064, err)
			}
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": map[string]bool{"stopped": true}})
	}
}

func PostChat(svc *chat.Service) app.HandlerFunc {
	stream := Chat(svc)
	stop := StopChatTurn(svc)
	return func(ctx context.Context, c *app.RequestContext) {
		switch strings.TrimSpace(c.Query("action")) {
		case "":
			stream(ctx, c)
		case "stop":
			stop(ctx, c)
		default:
			writeAPIError(c, 400, 40065, fmt.Errorf("unknown chat action %q", c.Query("action")))
		}
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
//	error   data={"message":"...","detail":"...","log_id":"...","recoverable":true}
//
// 一旦进入 SSE（响应头已发），出错只能通过 error 事件传达，不能再改 HTTP 状态码。
// chatHeartbeatInterval 是探活间隔。对端关闭后的第一次写往往还能进 socket 缓冲，
// 要到对方回 RST 之后的下一次写才报错，所以实际察觉最多要两个间隔。
const chatHeartbeatInterval = 15 * time.Second

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

		// 客户端断开时 Hertz 不会取消 ctx。浏览器一刷新，这一轮就没人接收了，却会
		// 一直跑到单轮超时，白烧配额还占着 codex 的 thread，让刷新后的页面 resume
		// 不上。定期写一条 SSE 注释探活：写不动说明对端已经走了，直接中止本轮。
		streamCtx, cancelStream := context.WithCancel(ctx)
		heartbeatStopped := make(chan struct{})
		go func() {
			defer close(heartbeatStopped)
			ticker := time.NewTicker(chatHeartbeatInterval)
			defer ticker.Stop()
			for {
				select {
				case <-streamCtx.Done():
					return
				case <-ticker.C:
					if err := w.WriteComment("ping"); err != nil {
						hlog.CtxInfof(ctx, "chat client went away, aborting the turn error=%+v", err)
						cancelStream()
						return
					}
				}
			}
		}()
		defer func() {
			cancelStream()
			<-heartbeatStopped
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

		if err := svc.Stream(streamCtx, req, emit); err != nil {
			// fail-fast：把错误作为 error 事件发出（此时响应头已发，无法再改状态码）。
			hlog.CtxErrorf(ctx, "chat stream failed error=%+v", err)
			data, marshalErr := json.Marshal(map[string]any{
				"message":     friendlyChatError(err),
				"detail":      strings.TrimSpace(err.Error()),
				"log_id":      observability.LogID(ctx),
				"recoverable": chatErrorRecoverable(err),
			})
			if marshalErr != nil {
				hlog.CtxErrorf(ctx, "marshal chat error event failed original_error=%+v marshal_error=%+v", err, marshalErr)
				data = []byte(`{"message":"这轮没有完成，可以重试或继续发送。","detail":"chat failed","recoverable":true}`)
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

func friendlyChatError(err error) string {
	text := strings.TrimSpace(err.Error())
	switch {
	case text == "":
		return "这轮没有完成，可以重试或继续发送。"
	case strings.Contains(text, "no rollout found for thread id"), strings.Contains(text, "missing thread.started"), strings.Contains(text, "belongs to another CLI"):
		return "这个会话暂时无法继续，已准备切换到新对话。"
	case strings.Contains(text, "previous chat turn"):
		return "上一轮还没有完全停止，请稍后重试。"
	case strings.Contains(text, "context canceled"), strings.Contains(text, "operation was aborted"):
		return "这轮回复已暂停。"
	default:
		return "这轮没有完成，可以重试或继续发送。"
	}
}

func chatErrorRecoverable(err error) bool {
	text := strings.TrimSpace(err.Error())
	return text != "" && (strings.Contains(text, "no rollout found for thread id") ||
		strings.Contains(text, "missing thread.started") ||
		strings.Contains(text, "belongs to another CLI"))
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

	allowedValues := map[string]bool{"message": true, "thread_id": true, "turn_id": true, "page_context": true, "user_open_id": true}
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

	if values := form.Value["turn_id"]; len(values) > 1 {
		return fail(fmt.Errorf("chat turn_id must appear at most once"))
	} else if len(values) == 1 {
		req.TurnID = strings.TrimSpace(values[0])
		if _, err := chat.ValidateTurnID(req.TurnID); err != nil {
			return fail(err)
		}
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
