package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"jarvis/internal/capture"
	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type interactiveMessageCapturer interface {
	CaptureInteractive(context.Context, capture.InteractiveMessage) (*domain.Message, error)
}

type interactiveTaskSubmitter interface {
	SubmitIdempotent(context.Context, taskcreate.Input) (*domain.Task, bool, error)
}

type interactiveTaskRelayRequest struct {
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

// RelayInteractiveTask is the authenticated machine boundary used by CC
// Connect after it has established that the bot was explicitly mentioned. It
// captures the raw message, freezes 25 recent messages, creates one Task and
// wakes M5 without involving M3.
func RelayInteractiveTask(capturer interactiveMessageCapturer, submitter interactiveTaskSubmitter, secret string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if capturer == nil || submitter == nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50323, fmt.Errorf("interactive Task relay is unavailable"))
			return
		}
		gotSecret := strings.TrimSpace(string(c.Request.Header.Peek(cardApprovalRelaySecretHeader)))
		wantSecret := strings.TrimSpace(secret)
		if wantSecret == "" || subtle.ConstantTimeCompare([]byte(gotSecret), []byte(wantSecret)) != 1 {
			writeAPIError(c, consts.StatusUnauthorized, 40123, fmt.Errorf("interactive Task relay authentication failed"))
			return
		}
		var request interactiveTaskRelayRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, err)
			return
		}
		message, err := capturer.CaptureInteractive(ctx, capture.InteractiveMessage{
			MessageID: request.MessageID, ChatID: request.ChatID, ChatMode: request.ChatMode,
			ChatName: request.ChatName, SenderOpenID: request.SenderOpenID, SenderName: request.SenderName,
			MessageType: request.MessageType, Content: request.Content, ContentRaw: request.ContentRaw,
			Mentions: request.Mentions, ParentID: request.ParentID, RootID: request.RootID,
			ThreadID: request.ThreadID, CreateTime: request.CreateTime,
		})
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, err)
			return
		}
		if message.GroupID == nil || *message.GroupID == 0 {
			writeAPIError(c, consts.StatusInternalServerError, 50033, fmt.Errorf("captured interactive message has no group_id"))
			return
		}
		rawSource, err := json.Marshal(request)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50033, fmt.Errorf("encode interactive source payload: %w", err))
			return
		}
		requestContext, err := json.Marshal(map[string]any{
			"entrypoint": "feishu_explicit_mention", "message_id": message.MessageID,
			"chat_id": message.ChatID, "thread_id": request.ThreadID, "root_id": request.RootID,
		})
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50033, fmt.Errorf("encode interactive request context: %w", err))
			return
		}
		occurrenceKey := message.MessageID
		task, created, err := submitter.SubmitIdempotent(ctx, taskcreate.Input{
			Title:      "处理飞书请求：" + truncateInteractiveTitle(message.Content, 64),
			ActionType: "direct_request", Target: message.Content,
			Background: requestContext, SourcePayload: rawSource,
			SourceType: taskcreate.SourceInteractive, SourceID: message.GroupID, OccurrenceKey: &occurrenceKey,
			ActorType: "feishu_user", EventDetail: map[string]any{"channel": "cc_connect", "message_id": message.MessageID},
			ChatID: message.ChatID, AnchorMessageID: message.MessageID,
			ConversationLimit: taskcreate.InteractiveConversationLimit,
		})
		if err != nil {
			switch {
			case errors.Is(err, taskcreate.ErrInvalidInput):
				writeAPIError(c, consts.StatusBadRequest, 40033, err)
			default:
				writeAPIError(c, consts.StatusInternalServerError, 50033, fmt.Errorf("create interactive Task: %w", err))
			}
			return
		}
		status := consts.StatusOK
		if created {
			status = consts.StatusCreated
		}
		c.JSON(status, map[string]any{"code": 0, "data": map[string]any{
			"task_id": task.ID, "status": task.Status, "created": created,
			"conversation_limit": taskcreate.InteractiveConversationLimit,
		}})
	}
}

func truncateInteractiveTitle(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}
