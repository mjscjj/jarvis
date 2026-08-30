package api

import (
	"context"
	"errors"
	"strings"

	"jarvis/internal/plugin"
	"jarvis/internal/scheduledtask"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type PluginService interface {
	List(context.Context) ([]plugin.View, error)
	Get(context.Context, string) (*plugin.View, error)
	Update(context.Context, string, plugin.UpdateInput) (*plugin.View, error)
	BeginAuthorization(context.Context, string) (*plugin.AuthStatus, error)
	CompleteAuthorization(context.Context, string, string) (*plugin.View, *plugin.AuthStatus, error)
	Trigger(context.Context, string) (*plugin.View, error)
}

func ListPlugins(service PluginService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		items, err := service.List(ctx)
		if err != nil {
			writePluginError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func GetPlugin(service PluginService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		view, err := service.Get(ctx, strings.TrimSpace(c.Param("plugin_id")))
		if err != nil {
			writePluginError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func UpdatePlugin(service PluginService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input plugin.UpdateInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40081, err)
			return
		}
		view, err := service.Update(ctx, strings.TrimSpace(c.Param("plugin_id")), input)
		if err != nil {
			writePluginError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func AuthorizePlugin(service PluginService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		status, err := service.BeginAuthorization(ctx, strings.TrimSpace(c.Param("plugin_id")))
		if err != nil {
			writePluginError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": status})
	}
}

type completePluginAuthorizationRequest struct {
	FlowID string `json:"flow_id"`
}

func CompletePluginAuthorization(service PluginService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request completePluginAuthorizationRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40081, err)
			return
		}
		if strings.TrimSpace(request.FlowID) == "" {
			writeAPIError(c, consts.StatusBadRequest, 40081, errors.New("flow_id is required"))
			return
		}
		view, status, err := service.CompleteAuthorization(
			ctx, strings.TrimSpace(c.Param("plugin_id")), request.FlowID,
		)
		if err != nil {
			writePluginError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{
			"authorization": status, "plugin": view,
		}})
	}
}

func TriggerPlugin(service PluginService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		view, err := service.Trigger(ctx, strings.TrimSpace(c.Param("plugin_id")))
		if err != nil {
			writePluginError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func writePluginError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, plugin.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40480, err)
	case errors.Is(err, plugin.ErrConflict), errors.Is(err, scheduledtask.ErrRunning):
		writeAPIError(c, consts.StatusConflict, 40980, err)
	case errors.Is(err, plugin.ErrDisabled), errors.Is(err, plugin.ErrAuthorizationRequired):
		writeAPIError(c, consts.StatusBadRequest, 40082, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50080, err)
	}
}
