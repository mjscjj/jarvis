package api

import (
	"context"
	"fmt"
	"net"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type SystemShutdowner interface {
	Shutdown(context.Context) error
}

func ShutdownSystem(service SystemShutdowner) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !isLoopbackAddress(c.RemoteAddr()) {
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

func isLoopbackAddress(address net.Addr) bool {
	if address == nil {
		return false
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
