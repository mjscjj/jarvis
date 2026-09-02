package api

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// GetWebConfig hands the browser the deployment facts it cannot read off its
// own address bar. public_base_url is where other people should open this
// deployment, so links meant for them keep the configured hostname even when
// the author browsed in over a raw IP. It is empty when the deployment has no
// address beyond the one the browser already used.
func GetWebConfig(publicBaseURL string) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"public_base_url": publicBaseURL}})
	}
}
