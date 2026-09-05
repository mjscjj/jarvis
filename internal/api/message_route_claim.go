package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/capture"
	"jarvis/internal/domain"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type messageRouteClaimer interface {
	CaptureRouteClaim(context.Context, capture.RouteClaimMessage) (*domain.Message, error)
}

type messageRouteClaimRequest struct {
	MessageID    string          `json:"message_id"`
	ChatID       string          `json:"chat_id"`
	ChatMode     string          `json:"chat_mode"`
	ChatName     string          `json:"chat_name"`
	SenderOpenID string          `json:"sender_open_id"`
	SenderName   string          `json:"sender_name"`
	MessageType  string          `json:"message_type"`
	Content      string          `json:"content"`
	ContentRaw   string          `json:"content_raw"`
	Mentions     json.RawMessage `json:"mentions"`
	ParentID     string          `json:"parent_id"`
	RootID       string          `json:"root_id"`
	ThreadID     string          `json:"thread_id"`
	CreateTime   int64           `json:"create_time"`
}

// ClaimMessageRoute is the authenticated machine boundary used by an
// interactive transport immediately before it consumes a Feishu message. It
// records routing ownership only; no Todo or Task is created.
func ClaimMessageRoute(claimer messageRouteClaimer, secret string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if claimer == nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50323, fmt.Errorf("message route claim is unavailable"))
			return
		}
		gotSecret := strings.TrimSpace(string(c.Request.Header.Peek(jarvisRelaySecretHeader)))
		wantSecret := strings.TrimSpace(secret)
		if wantSecret == "" || subtle.ConstantTimeCompare([]byte(gotSecret), []byte(wantSecret)) != 1 {
			writeAPIError(c, consts.StatusUnauthorized, 40123, fmt.Errorf("message route claim authentication failed"))
			return
		}
		var request messageRouteClaimRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, err)
			return
		}
		message, err := claimer.CaptureRouteClaim(ctx, capture.RouteClaimMessage{
			MessageID: request.MessageID, ChatID: request.ChatID, ChatMode: request.ChatMode,
			ChatName: request.ChatName, SenderOpenID: request.SenderOpenID, SenderName: request.SenderName,
			MessageType: request.MessageType, Content: request.Content, ContentRaw: request.ContentRaw,
			Mentions: request.Mentions, ParentID: request.ParentID, RootID: request.RootID,
			ThreadID: request.ThreadID, CreateTime: request.CreateTime,
		})
		if err != nil {
			if errors.Is(err, capture.ErrInvalidRouteClaim) {
				writeAPIError(c, consts.StatusBadRequest, 40033, err)
				return
			}
			writeAPIError(c, consts.StatusInternalServerError, 50033, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{
			"message_id": message.MessageID, "claimed": message.ExtractionSkipped,
		}})
	}
}
