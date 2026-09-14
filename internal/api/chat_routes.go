package api

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/chat"
	okrAuth "jarvis/internal/okrworkspace/auth"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func scopeOKRChat(ctx context.Context, c *app.RequestContext) {
	value, exists := c.Get(okrIdentityContextKey)
	user, ok := value.(okrAuth.User)
	if !exists || !ok {
		writeAPIError(c, consts.StatusInternalServerError, 50089, fmt.Errorf("OKR chat identity is missing"))
		c.Abort()
		return
	}
	ownerID := strings.TrimSpace(user.UnionID)
	if ownerID == "" && user.OpenID == "jarvis" {
		ownerID = "jarvis" // Local instances with OKR login disabled.
	}
	if ownerID == "" {
		writeAPIError(c, consts.StatusInternalServerError, 50089, fmt.Errorf("OKR chat identity has no stable ID"))
		c.Abort()
		return
	}
	c.Next(chat.WithOwner(ctx, ownerID))
}

func registerChatRoutes(h *server.Hertz, prefix string, service *chat.Service, guards ...app.HandlerFunc) {
	bind := func(factory func(*chat.Service) app.HandlerFunc) []app.HandlerFunc {
		return append(append([]app.HandlerFunc{}, guards...), factory(service))
	}
	h.GET(prefix+"/agents", bind(ListChatAgents)...)
	h.GET(prefix+"/agents/:agent_id/models", bind(ListChatModels)...)
	h.GET(prefix+"/sessions", bind(ListChatSessions)...)
	h.POST(prefix+"/sessions", bind(CreateChatSession)...)
	h.GET(prefix+"/sessions/:session_id", bind(GetChatSession)...)
	h.PATCH(prefix+"/sessions/:session_id", bind(UpdateChatSession)...)
	h.DELETE(prefix+"/sessions/:session_id", bind(DeleteChatSession)...)
	h.POST(prefix+"/sessions/:session_id/messages", bind(StreamChatSession)...)
	h.POST(prefix+"/sessions/:session_id/cancel", bind(CancelChatSession)...)
	h.POST(prefix+"/sessions/:session_id/attachments", bind(UploadChatAttachment)...)
	h.DELETE(prefix+"/sessions/:session_id/attachments/:attachment_id", bind(DeleteChatAttachment)...)
	h.GET(prefix+"/attachments/:attachment_id/content", bind(DownloadChatAttachment)...)
}
