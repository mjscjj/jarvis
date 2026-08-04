package capture

import (
	"encoding/json"
	"fmt"
	"strings"
)

const cardActionEventType = "card.action.trigger"

// CardActionEvent is the flat NDJSON object emitted by
// `lark-cli event consume card.action.trigger`. Only the fields Jarvis needs to
// map a button click onto an existing approval are modeled; the rest of the
// documented payload is intentionally ignored.
type CardActionEvent struct {
	Type       string `json:"type"`
	EventID    string `json:"event_id"`
	OperatorID string `json:"operator_id"`
	MessageID  string `json:"message_id"`
	ChatID     string `json:"chat_id"`
	// Token is the delayed-update token (valid 30 min, max 2 uses) used to update
	// the card in place after the click lands.
	Token string `json:"token"`
	// ActionTag is the component type that fired; approval buttons are `button`.
	ActionTag string `json:"action_tag"`
	// ActionValue is the developer-defined value carried on the button, serialized
	// to a JSON string, e.g. `{"action":"approve","task_id":5}`.
	ActionValue string `json:"action_value"`
	// FormValue is non-empty for a button used as a form submit. Approval cards
	// use standalone buttons only; rejecting form submits prevents unrelated card
	// interactions from reaching the approval adapter.
	FormValue string `json:"form_value"`
}

// CardApprovalAction is the decoded intent behind an approval-card button. The
// button carries only which task and whether to approve or reject; the click
// itself is the human's approval, exactly like the HTTP approve/reject handlers.
type CardApprovalAction struct {
	Action string `json:"action"`
	TaskID uint64 `json:"task_id"`
}

// AuthorizeCardApproval decodes and validates one card.action.trigger event for
// the approval flow. It enforces the only hard boundaries that must hold before
// any state changes: the click came from the principal, it names a real action,
// and it targets a positive task id. It deliberately does NOT judge risk or look
// at the task's action_type — whether approve/reject buttons even exist on the
// card is M5's decision, and a click that arrives here already means "landed".
func AuthorizeCardApproval(event CardActionEvent, principalOpenID string) (CardApprovalAction, error) {
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return CardApprovalAction{}, fmt.Errorf("card action principal open_id is not configured")
	}
	if strings.TrimSpace(event.OperatorID) != principalOpenID {
		return CardApprovalAction{}, fmt.Errorf("card action operator_id=%q is not the principal", event.OperatorID)
	}
	if strings.TrimSpace(event.ActionTag) != "button" {
		return CardApprovalAction{}, fmt.Errorf("card action tag=%q is not button", event.ActionTag)
	}
	if strings.TrimSpace(event.FormValue) != "" {
		return CardApprovalAction{}, fmt.Errorf("card approval button must not submit a form")
	}
	raw := strings.TrimSpace(event.ActionValue)
	if raw == "" {
		return CardApprovalAction{}, fmt.Errorf("card action value is empty")
	}
	var action CardApprovalAction
	if err := json.Unmarshal([]byte(raw), &action); err != nil {
		return CardApprovalAction{}, fmt.Errorf("decode card action value %q: %w", raw, err)
	}
	action.Action = strings.TrimSpace(action.Action)
	switch action.Action {
	case "approve", "reject":
	default:
		return CardApprovalAction{}, fmt.Errorf("card action %q is not approve or reject", action.Action)
	}
	if action.TaskID == 0 {
		return CardApprovalAction{}, fmt.Errorf("card action task_id must be positive")
	}
	return action, nil
}

func validateCardActionEvent(event CardActionEvent) error {
	if strings.TrimSpace(event.Type) != cardActionEventType {
		return fmt.Errorf("card action event type=%q, want %s", event.Type, cardActionEventType)
	}
	if strings.TrimSpace(event.OperatorID) == "" {
		return fmt.Errorf("card action operator_id is empty")
	}
	return nil
}
