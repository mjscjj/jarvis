package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strings"

	"jarvis/internal/cardapproval"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const cardApprovalRelaySecretHeader = "X-Jarvis-Relay-Secret"

// CardApprovalProcessor is the strict machine boundary behind the CC Connect
// relay. The implementation still owns all task/proposal/version
// validation; CC Connect only transports the authenticated Feishu callback.
type CardApprovalProcessor interface {
	ProcessCardAction(context.Context, cardapproval.CardActionEvent) (json.RawMessage, error)
}

type cardApprovalRelayRequest struct {
	EventID     string         `json:"event_id"`
	OperatorID  string         `json:"operator_id"`
	MessageID   string         `json:"message_id"`
	ChatID      string         `json:"chat_id"`
	ActionTag   string         `json:"action_tag"`
	ActionValue map[string]any `json:"action_value"`
}

// RelayCardApproval accepts only authenticated localhost traffic registered by
// CC Connect. It returns a Card 2.0 outcome fragment for CC Connect to merge
// into the original card and answer Feishu synchronously.
func RelayCardApproval(processor CardApprovalProcessor, secret string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if processor == nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50322, fmt.Errorf("card approval relay is unavailable"))
			return
		}
		gotSecret := strings.TrimSpace(string(c.Request.Header.Peek(cardApprovalRelaySecretHeader)))
		wantSecret := strings.TrimSpace(secret)
		if wantSecret == "" || subtle.ConstantTimeCompare([]byte(gotSecret), []byte(wantSecret)) != 1 {
			writeAPIError(c, consts.StatusUnauthorized, 40122, fmt.Errorf("card approval relay authentication failed"))
			return
		}
		var request cardApprovalRelayRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40032, err)
			return
		}
		namespace, _ := request.ActionValue["action"].(string)
		decision, _ := request.ActionValue["decision"].(string)
		if strings.TrimSpace(namespace) != "jarvis_approval" {
			writeAPIError(c, consts.StatusBadRequest, 40032, fmt.Errorf("action_value.action must be jarvis_approval"))
			return
		}
		actionValue, err := json.Marshal(map[string]any{
			"action":  decision,
			"task_id": request.ActionValue["task_id"],
			"version": request.ActionValue["version"],
		})
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40032, fmt.Errorf("encode action_value: %w", err))
			return
		}
		card, err := processor.ProcessCardAction(ctx, cardapproval.CardActionEvent{
			EventID:     request.EventID,
			OperatorID:  request.OperatorID,
			MessageID:   request.MessageID,
			ChatID:      request.ChatID,
			ActionTag:   request.ActionTag,
			ActionValue: string(actionValue),
		})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "card": json.RawMessage(card)})
	}
}
