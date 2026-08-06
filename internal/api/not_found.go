package api

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// apiNotFound keeps unknown API requests inside the API protocol boundary.
// It intentionally does not redirect or alias removed endpoints.
func apiNotFound() app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		writeAPIError(c, consts.StatusNotFound, 40400, fmt.Errorf("api route not found"))
	}
}
