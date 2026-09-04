package api

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

const immutableAssetCacheControl = "public, max-age=31536000, immutable"

// StaticAssetCacheHeaders lets browsers and the public gateway keep Vite's
// content-hashed build assets. Other paths, especially APIs and index.html,
// retain their existing cache semantics.
func StaticAssetCacheHeaders() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if strings.HasPrefix(string(c.Request.URI().PathOriginal()), "/assets/") {
			c.Response.Header.Set("Cache-Control", immutableAssetCacheControl)
		}
		c.Next(ctx)
	}
}
