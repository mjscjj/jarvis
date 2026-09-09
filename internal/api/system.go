package api

import (
	"context"
	"fmt"

	"jarvis/internal/authn"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type SystemShutdowner interface {
	Shutdown(context.Context) error
}

func ShutdownSystem(service SystemShutdowner) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !authn.IsLoopbackRequest(c) {
			writeAPIError(c, consts.StatusForbidden, 40390, fmt.Errorf("system shutdown is only available from this machine"))
			return
		}
		if err := service.Shutdown(ctx); err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50090, err)
			return
		}
		c.JSON(consts.StatusAccepted, map[string]any{
			"code": 0,
			"data": map[string]any{"stopping": true},
		})
	}
}
