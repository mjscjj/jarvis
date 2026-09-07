package authn

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const CookieName = "jarvis_session"

// BrowserMiddleware protects browser-originated management API requests. Local
// CLI and Agent calls carry no Fetch Metadata headers and remain trusted local
// machine traffic until those callers have a dedicated machine credential.
func BrowserMiddleware(service *Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())
		if !service.Enabled() || !isProtectedBrowserPath(path) || isPublicPath(path) || !isBrowserRequest(c) {
			c.Next(ctx)
			return
		}
		if _, ok := service.Authenticate(string(c.Cookie(CookieName))); ok {
			c.Next(ctx)
			return
		}
		c.AbortWithStatusJSON(consts.StatusUnauthorized, map[string]any{
			"code": 401,
			"msg":  "请先使用字节身份登录",
		})
	}
}

func isProtectedBrowserPath(path string) bool {
	return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/okr-assets/")
}

func isPublicPath(path string) bool {
	return strings.HasPrefix(path, "/api/auth/") ||
		path == "/api/agent-identity" ||
		path == "/api/clues"
}

func isBrowserRequest(c *app.RequestContext) bool {
	return len(c.Request.Header.Peek("Sec-Fetch-Mode")) > 0 ||
		len(c.Request.Header.Peek("Origin")) > 0
}
