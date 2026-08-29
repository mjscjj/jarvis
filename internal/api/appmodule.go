package api

import (
	"context"
	"errors"

	"jarvis/internal/appmodule"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type AppModuleService interface {
	List(ctx context.Context) ([]appmodule.View, error)
	Update(ctx context.Context, key string, input appmodule.Input) (*appmodule.View, error)
}

func ListAppModules(service AppModuleService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		items, err := service.List(ctx)
		if err != nil {
			writeAppModuleError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func UpdateAppModule(service AppModuleService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input appmodule.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40061, err)
			return
		}
		view, err := service.Update(ctx, c.Param("module_key"), input)
		if err != nil {
			writeAppModuleError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func writeAppModuleError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, appmodule.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40062, err)
	case errors.Is(err, appmodule.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40460, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50060, err)
	}
}
