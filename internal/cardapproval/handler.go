// Package cardapproval maps authenticated Feishu approval-card callbacks from
// CC Connect to the existing Task approve/reject actions. The card carries the
// exact durable Task version it was created for, so stale or repeated clicks
// fail through the existing optimistic lock without polling or extra state.
package cardapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"jarvis/internal/execute"
)

// Approver lands or declines an awaiting_approval Task. *execute.AgentExecutor
// satisfies this via KickApprove/Reject.
type Approver interface {
	KickApprove(ctx context.Context, taskID uint64, expectedVersion int32) (*execute.ExecuteResult, error)
	Reject(ctx context.Context, taskID uint64, expectedVersion int32, reason string) (*execute.ExecuteResult, error)
}

// CardActionEvent is the strict relay payload needed to land one Feishu click.
// CC Connect owns the Feishu callback connection.
type CardActionEvent struct {
	EventID     string
	OperatorID  string
	MessageID   string
	ChatID      string
	ActionTag   string
	ActionValue string
	FormValue   string
}

type cardApprovalAction struct {
	Action  string `json:"action"`
	TaskID  uint64 `json:"task_id"`
	Version int32  `json:"version"`
}

type Handler struct {
	approver      Approver
	principalOpen string
	logger        *log.Logger
}

// NewRelayHandler builds the approval processor behind the authenticated
// localhost relay. Jarvis never opens a Feishu event connection itself.
func NewRelayHandler(approver Approver, principalOpenID string, logger *log.Logger) (*Handler, error) {
	if approver == nil {
		return nil, fmt.Errorf("card approval approver is nil")
	}
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return nil, fmt.Errorf("card approval principal open_id is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("card approval logger is nil")
	}
	return &Handler{approver: approver, principalOpen: principalOpenID, logger: logger}, nil
}

// ProcessCardAction immediately lands one version-bound callback and returns a
// small outcome fragment. CC Connect merges it into the original card, keeping
// the proposal text visible while removing the decision buttons.
func (h *Handler) ProcessCardAction(ctx context.Context, event CardActionEvent) (json.RawMessage, error) {
	action, err := authorizeCardApproval(event, h.principalOpen)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", execute.ErrInvalidInput, err)
	}

	switch action.Action {
	case "approve":
		_, err = h.approver.KickApprove(ctx, action.TaskID, action.Version)
	case "reject":
		_, err = h.approver.Reject(ctx, action.TaskID, action.Version, "委托人在飞书卡片上驳回")
	default:
		return nil, fmt.Errorf("%w: unsupported card approval action %q", execute.ErrInvalidInput, action.Action)
	}
	if err != nil {
		return h.onLandFailed(action, err)
	}
	if action.Action == "approve" {
		return cardNoticeText("✅ 已同意，正在处理。"), nil
	}
	return cardNoticeText("已驳回，不会执行。"), nil
}

func authorizeCardApproval(event CardActionEvent, principalOpenID string) (cardApprovalAction, error) {
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return cardApprovalAction{}, fmt.Errorf("card action principal open_id is not configured")
	}
	if strings.TrimSpace(event.OperatorID) != principalOpenID {
		return cardApprovalAction{}, fmt.Errorf("card action operator_id=%q is not the principal", event.OperatorID)
	}
	if strings.TrimSpace(event.ActionTag) != "button" {
		return cardApprovalAction{}, fmt.Errorf("card action tag=%q is not button", event.ActionTag)
	}
	if strings.TrimSpace(event.FormValue) != "" {
		return cardApprovalAction{}, fmt.Errorf("card approval button must not submit a form")
	}
	raw := strings.TrimSpace(event.ActionValue)
	if raw == "" {
		return cardApprovalAction{}, fmt.Errorf("card action value is empty")
	}
	var action cardApprovalAction
	if err := json.Unmarshal([]byte(raw), &action); err != nil {
		return cardApprovalAction{}, fmt.Errorf("decode card action value %q: %w", raw, err)
	}
	action.Action = strings.TrimSpace(action.Action)
	if action.Action != "approve" && action.Action != "reject" {
		return cardApprovalAction{}, fmt.Errorf("card action %q is not approve or reject", action.Action)
	}
	if action.TaskID == 0 {
		return cardApprovalAction{}, fmt.Errorf("card action task_id must be positive")
	}
	if action.Version <= 0 {
		return cardApprovalAction{}, fmt.Errorf("card action version must be positive")
	}
	return action, nil
}

func (h *Handler) onLandFailed(action cardApprovalAction, err error) (json.RawMessage, error) {
	if errors.Is(err, execute.ErrVersionConflict) || errors.Is(err, execute.ErrInvalidTransition) {
		h.logger.Printf("job=card-approval status=info action=%s task_id=%d version=%d skipped=already-handled: %v", action.Action, action.TaskID, action.Version, err)
		return cardNoticeText("这条审批已经处理过了，请去后台确认。"), nil
	}
	return cardNoticeText("处理没成功，请去后台重试。"), fmt.Errorf("card approval %s task_id=%d version=%d: %w", action.Action, action.TaskID, action.Version, err)
}

// cardNoticeText is an outcome fragment. CC Connect owns merging this fragment
// into the original card so its proposal copy remains available as audit text.
func cardNoticeText(text string) json.RawMessage {
	card := map[string]any{
		"schema": "2.0",
		"body": map[string]any{
			"direction": "vertical",
			"padding":   "12px 12px 12px 12px",
			"elements": []any{
				map[string]any{"tag": "markdown", "content": text},
			},
		},
	}
	raw, _ := json.Marshal(card)
	return raw
}
