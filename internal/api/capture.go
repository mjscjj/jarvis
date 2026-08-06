package api

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/capture"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// 调试面板"手动触发"：直接复用 M1 capture service 的采集入口，供本地手动跑一轮
// 采集，无需等 cron。均为同步调用，采集完成才返回。

// DiscoverChatsManually 手动跑一次会话发现（等价 CLI -discover-once）。
func DiscoverChatsManually(svc *capture.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if err := svc.DiscoverChats(ctx); err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50030, fmt.Errorf("discover chats failed: %s", strings.TrimSpace(err.Error())))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"action": "discover", "ok": true}})
	}
}

// ScanRelatedManually 手动采集所有已监听会话（等价 CLI 一次性全量 related 扫描）。
func ScanRelatedManually(svc *capture.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if err := svc.ScanRelated(ctx); err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50031, fmt.Errorf("scan related chats failed: %s", strings.TrimSpace(err.Error())))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"action": "scan_related", "ok": true}})
	}
}

type scanChatRequest struct {
	ChatID string `json:"chat_id"`
}

// ScanChatManually 手动采集指定 chat_id（等价 CLI -scan-chat）。
func ScanChatManually(svc *capture.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request scanChatRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40030, err)
			return
		}
		chatID := strings.TrimSpace(request.ChatID)
		if chatID == "" {
			writeAPIError(c, consts.StatusBadRequest, 40030, fmt.Errorf("chat_id is required"))
			return
		}
		if err := svc.ScanChatNow(ctx, chatID); err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50032, fmt.Errorf("scan chat failed: %s", strings.TrimSpace(err.Error())))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"action": "scan_chat", "chat_id": chatID, "ok": true}})
	}
}
