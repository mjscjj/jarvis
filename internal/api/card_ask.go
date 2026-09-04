package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strings"

	"jarvis/internal/cardask"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const jarvisRelaySecretHeader = "X-Jarvis-Relay-Secret"

// CardAskProcessor is the strict machine boundary behind the CC Connect relay.
// The implementation still owns all task/version validation; CC Connect only
// transports the authenticated Feishu callback.
type CardAskProcessor interface {
	ProcessCardAction(context.Context, cardask.CardActionEvent) (json.RawMessage, error)
}

type cardAskRelayRequest struct {
	EventID     string         `json:"event_id"`
	OperatorID  string         `json:"operator_id"`
	MessageID   string         `json:"message_id"`
	ChatID      string         `json:"chat_id"`
	ActionTag   string         `json:"action_tag"`
	ActionValue map[string]any `json:"action_value"`
	FormValue   map[string]any `json:"form_value"`
}

// RelayCardAsk accepts only authenticated localhost traffic registered by CC
// Connect. It returns a Card 2.0 card for CC Connect to replace the original
// message with, answering Feishu synchronously.
//
// The route, the header and the action namespace all still say "approval":
// they are the wire contract with an installed CC Connect build, and renaming
// them would force a re-patch and reinstall on every machine for nothing.
func RelayCardAsk(processor CardAskProcessor, secret string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if processor == nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50322, fmt.Errorf("card ask relay is unavailable"))
			return
		}
		gotSecret := strings.TrimSpace(string(c.Request.Header.Peek(jarvisRelaySecretHeader)))
		wantSecret := strings.TrimSpace(secret)
		if wantSecret == "" || subtle.ConstantTimeCompare([]byte(gotSecret), []byte(wantSecret)) != 1 {
			writeAPIError(c, consts.StatusUnauthorized, 40122, fmt.Errorf("card ask relay authentication failed"))
			return
		}
		var request cardAskRelayRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40032, err)
			return
		}
		actionValue, err := json.Marshal(request.ActionValue)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40032, fmt.Errorf("encode action_value: %w", err))
			return
		}
		card, err := processor.ProcessCardAction(ctx, cardask.CardActionEvent{
			EventID:     request.EventID,
			OperatorID:  request.OperatorID,
			MessageID:   request.MessageID,
			ChatID:      request.ChatID,
			ActionTag:   request.ActionTag,
			ActionValue: string(actionValue),
			FormValue:   request.FormValue,
		})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "card": json.RawMessage(card)})
	}
}
