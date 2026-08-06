package api

import (
	"context"
	"errors"
	"fmt"

	"jarvis/internal/config"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type RuntimeSettingsService interface {
	Get(ctx context.Context) (*config.RuntimeSettingsView, error)
	Update(ctx context.Context, input config.RuntimeSettings) (*config.RuntimeSettingsView, error)
}

func GetRuntimeSettings(service RuntimeSettingsService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		view, err := service.Get(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50090, fmt.Errorf("get runtime settings failed: %w", err))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func UpdateRuntimeSettings(service RuntimeSettingsService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input config.RuntimeSettings
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40090, err)
			return
		}
		view, err := service.Update(ctx, input)
		if err != nil {
			if errors.Is(err, config.ErrInvalidRuntimeSettings) {
				writeAPIError(c, consts.StatusBadRequest, 40091, err)
				return
			}
			writeAPIError(c, consts.StatusInternalServerError, 50090, fmt.Errorf("save runtime settings failed: %w", err))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}
