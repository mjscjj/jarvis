package api

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"jarvis/internal/chat"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/gorm"
)

// GetChatRuntimeConfig lets the already-loaded browser discover the separate
// chat listener. The browser keeps its own current hostname so LAN access does
// not accidentally receive a loopback-only URL.
func GetChatRuntimeConfig(chatAddr string) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		_, port, err := net.SplitHostPort(strings.TrimSpace(chatAddr))
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50062, fmt.Errorf("invalid chat address: %w", err))
			return
		}
		parsed, err := strconv.Atoi(port)
		if err != nil || parsed < 1 || parsed > 65535 {
			writeAPIError(c, consts.StatusInternalServerError, 50062, fmt.Errorf("invalid chat port %q", port))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"port": parsed}})
	}
}

// RegisterChatSidecar exposes only chat and health routes. Live business
// context is read from the main database; all mutations still use main APIs.
func RegisterChatSidecar(h *server.Hertz, svc *chat.Service, db *gorm.DB) error {
	if h == nil || svc == nil || db == nil {
		return fmt.Errorf("chat sidecar dependencies must not be nil")
	}
	h.Use(observability.Middleware(), chatSameHostCORS())
	h.GET("/healthz", HealthForService(db, "jarvis-chat-server"))
	h.POST("/api/chat", Chat(svc))
	h.GET("/api/chat", GetChatHistory(svc))
	h.GET("/api/chat/:thread_id", GetChatHistory(svc))
	h.OPTIONS("/api/chat", func(_ context.Context, c *app.RequestContext) {
		c.Status(consts.StatusNoContent)
	})
	return nil
}

func chatSameHostCORS() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		origin := strings.TrimSpace(string(c.Request.Header.Peek("Origin")))
		if origin != "" {
			if !allowedChatOrigin(origin, string(c.Host())) {
				c.AbortWithStatus(consts.StatusForbidden)
				return
			}
			c.Response.Header.Set("Access-Control-Allow-Origin", origin)
			c.Response.Header.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Response.Header.Set("Access-Control-Allow-Headers", "Content-Type")
			c.Response.Header.Set("Vary", "Origin")
		}
		c.Next(ctx)
	}
}

func allowedChatOrigin(origin, requestHost string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	requestURL, err := url.Parse("//" + requestHost)
	if err != nil || requestURL.Hostname() == "" || !strings.EqualFold(parsed.Hostname(), requestURL.Hostname()) {
		return false
	}
	return true
}
