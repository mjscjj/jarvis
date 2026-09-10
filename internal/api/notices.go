package api

import (
	"context"
	"errors"

	"jarvis/internal/notice"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func NoticePrincipal(service *notice.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if service == nil {
			c.JSON(consts.StatusServiceUnavailable, map[string]any{"code": 503, "message": "principal notice service unavailable"})
			return
		}
		result, err := service.Send(ctx, c.Request.Body())
		if err != nil {
			status := consts.StatusInternalServerError
			if errors.Is(err, notice.ErrInvalidInput) {
				status = consts.StatusBadRequest
			}
			// Partial delivery receipts must survive HTTP errors and CLI retries.
			c.JSON(status, map[string]any{"code": status, "message": err.Error(), "data": result})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
