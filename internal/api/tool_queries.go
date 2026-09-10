package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/toolquery"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type ToolQueryService interface {
	ListMessages(context.Context, toolquery.MessageFilter) ([]toolquery.MessageView, error)
	GetMessage(context.Context, uint64) (*toolquery.MessageDetailView, error)
	GetTodoEvent(context.Context, uint64) (*toolquery.TodoEventView, error)
	GetTaskEvent(context.Context, uint64) (*toolquery.TaskEventView, error)
	ListResources(context.Context, toolquery.ResourceFilter) ([]toolquery.ResourceSummary, error)
	GetResource(context.Context, uint64) (*toolquery.ResourceView, error)
}

func ListToolMessages(service ToolQueryService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, err := positiveQueryInt(c.Query("limit"), 20, "limit")
		if err != nil || limit > 100 {
			if err == nil {
				err = fmt.Errorf("limit must not exceed 100")
			}
			writeAPIError(c, consts.StatusBadRequest, 40070, err)
			return
		}
		from, err := optionalRFC3339(c.Query("from"), "from")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40070, err)
			return
		}
		until, err := optionalRFC3339(c.Query("until"), "until")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40070, err)
			return
		}
		var messageIDs []string
		if value := c.Query("message_ids"); value != "" {
			messageIDs = strings.Split(value, ",")
		}
		items, err := service.ListMessages(ctx, toolquery.MessageFilter{
			MessageIDs: messageIDs,
			ChatID:     c.Query("chat_id"), SenderOpenID: c.Query("sender_open_id"),
			Keyword: c.Query("keyword"), From: from, Until: until, Limit: limit,
		})
		if err != nil {
			writeToolQueryError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func GetToolMessage(service ToolQueryService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := positivePathID(c.Param("message_id"), "message_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40071, err)
			return
		}
		view, err := service.GetMessage(ctx, id)
		if err != nil {
			writeToolQueryError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func GetToolTodoEvent(service ToolQueryService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := positivePathID(c.Param("event_id"), "event_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40071, err)
			return
		}
		view, err := service.GetTodoEvent(ctx, id)
		if err != nil {
			writeToolQueryError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func GetToolTaskEvent(service ToolQueryService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := positivePathID(c.Param("event_id"), "event_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40071, err)
			return
		}
		view, err := service.GetTaskEvent(ctx, id)
		if err != nil {
			writeToolQueryError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func ListCapturedResources(service ToolQueryService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, err := positiveQueryInt(c.Query("limit"), 20, "limit")
		if err != nil || limit > 100 {
			if err == nil {
				err = fmt.Errorf("limit must not exceed 100")
			}
			writeAPIError(c, consts.StatusBadRequest, 40070, err)
			return
		}
		items, err := service.ListResources(ctx, toolquery.ResourceFilter{
			ChatID: c.Query("chat_id"), MessageID: c.Query("message_id"),
			ResourceType: c.Query("resource_type"), Keyword: c.Query("keyword"), Limit: limit,
		})
		if err != nil {
			writeToolQueryError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func GetCapturedResource(service ToolQueryService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := positivePathID(c.Param("resource_id"), "resource_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40071, err)
			return
		}
		view, err := service.GetResource(ctx, id)
		if err != nil {
			writeToolQueryError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func positivePathID(raw, field string) (uint64, error) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", field)
	}
	return id, nil
}

func optionalRFC3339(raw, field string) (*time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be RFC3339: %w", field, err)
	}
	return &parsed, nil
}

func writeToolQueryError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, toolquery.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40070, err)
	case errors.Is(err, toolquery.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40470, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50070, err)
	}
}
