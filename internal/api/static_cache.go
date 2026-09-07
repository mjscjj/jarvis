package api

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

const (
	immutableAssetCacheControl        = "public, max-age=31536000, immutable"
	privateImmutableAssetCacheControl = "private, max-age=31536000, immutable"
	webDocumentCacheControl           = "no-cache, must-revalidate"
)

// StaticAssetCacheHeaders keeps Vite's content-hashed assets while making the
// HTML entry revalidate on every visit. A cached entry can otherwise keep
// asking for chunks from an older deployment.
func StaticAssetCacheHeaders() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Request.URI().PathOriginal())
		if strings.HasPrefix(path, "/assets/") {
			c.Response.Header.Set("Cache-Control", immutableAssetCacheControl)
		} else if strings.HasPrefix(path, "/okr-assets/") {
			c.Response.Header.Set("Cache-Control", privateImmutableAssetCacheControl)
		} else if path == "/" || path == "/index.html" {
			c.Response.Header.Set("Cache-Control", webDocumentCacheControl)
		}
		c.Next(ctx)
	}
}
