package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/chat"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func ListChatAgents(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": service.ListAgents(ctx)}})
	}
}

func ListChatModels(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		items, err := service.ListModels(ctx, c.Param("agent_id"))
		if err != nil {
			if errors.Is(err, chat.ErrInvalidInput) {
				writeAPIError(c, consts.StatusBadRequest, 40060, err)
				return
			}
			writeAPIError(c, consts.StatusBadGateway, 50260, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func ListChatSessions(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		archived, _ := strconv.ParseBool(strings.TrimSpace(c.Query("archived")))
		items, err := service.ListSessions(ctx, c.Query("query"), archived)
		if err != nil {
			writeChatError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func CreateChatSession(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input chat.CreateSessionInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, 400, 40060, err)
			return
		}
		view, err := service.CreateSession(ctx, input)
		if err != nil {
			writeChatError(c, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": view})
	}
}

func GetChatSession(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		view, err := service.GetSession(ctx, c.Param("session_id"))
		if err != nil {
			writeChatError(c, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": view})
	}
}

func UpdateChatSession(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input chat.UpdateSessionInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, 400, 40060, err)
			return
		}
		view, err := service.UpdateSession(ctx, c.Param("session_id"), input)
		if err != nil {
			writeChatError(c, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": view})
	}
}

func DeleteChatSession(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if err := service.DeleteSession(ctx, c.Param("session_id")); err != nil {
			writeChatError(c, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": map[string]any{"deleted": true}})
	}
}

func StreamChatSession(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		ctx = observability.FromRequestContext(ctx, c)
		var input chat.SendInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, 400, 40060, err)
			return
		}
		w := newSSEWriter(c)
		defer func() {
			if err := w.Close(); err != nil {
				hlog.CtxErrorf(ctx, "close chat session stream failed error=%+v", err)
			}
		}()
		emit := func(ev chat.Event) error {
			if ev.Kind == chat.EventAccepted {
				return w.WriteEvent("accepted", []byte(`{}`))
			}
			if ev.Kind == chat.EventThread {
				raw, _ := json.Marshal(map[string]string{"thread_id": ev.ThreadID})
				return w.WriteEvent("thread", raw)
			}
			if ev.Kind == chat.EventDelta {
				raw, _ := json.Marshal(map[string]string{"text": ev.Text})
				return w.WriteEvent("delta", raw)
			}
			return fmt.Errorf("unknown chat event %s", ev.Kind)
		}
		err := service.StreamSession(ctx, c.Param("session_id"), input, emit)
		if err != nil {
			event := "error"
			if errors.Is(err, context.Canceled) {
				event = "stopped"
			}
			raw, _ := json.Marshal(map[string]string{"message": err.Error()})
			_ = w.WriteEvent(event, raw)
			return
		}
		_ = w.WriteEvent("done", []byte("{}"))
	}
}

func CancelChatSession(service *chat.Service) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"code": 0, "data": map[string]any{"canceled": service.CancelSession(c.Param("session_id"))}})
	}
}

func UploadChatAttachment(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		file, err := c.FormFile("file")
		if err != nil {
			writeAPIError(c, 400, 40060, fmt.Errorf("file is required: %w", err))
			return
		}
		view, err := service.SaveUpload(ctx, c.Param("session_id"), file.Filename, file.Header.Get("Content-Type"), file.Size, func(path string) error { return c.SaveUploadedFile(file, path) })
		if err != nil {
			writeChatError(c, err)
			return
		}
		c.JSON(201, map[string]any{"code": 0, "data": view})
	}
}

func DeleteChatAttachment(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if err := service.DeletePendingAttachment(ctx, c.Param("session_id"), c.Param("attachment_id")); err != nil {
			writeChatError(c, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": map[string]any{"deleted": true}})
	}
}

func DownloadChatAttachment(service *chat.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		record, err := service.GetAttachment(ctx, c.Param("attachment_id"))
		if err != nil {
			writeChatError(c, err)
			return
		}
		c.Response.Header.SetContentType(record.View.MIMEType)
		c.FileAttachment(record.LocalPath, record.View.Name)
	}
}

func writeChatError(c *app.RequestContext, err error) {
	if errors.Is(err, chat.ErrNotFound) {
		writeAPIError(c, 404, 40460, err)
		return
	}
	if errors.Is(err, chat.ErrInvalidInput) {
		writeAPIError(c, 400, 40060, err)
		return
	}
	if errors.Is(err, chat.ErrConflict) {
		writeAPIError(c, consts.StatusConflict, 40960, err)
		return
	}
	writeAPIError(c, 500, 50060, err)
}
