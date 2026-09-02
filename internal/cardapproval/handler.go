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
	Supplement(ctx context.Context, input execute.SupplementInput) (*execute.TaskView, error)
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
	FormValue   map[string]any
}

type cardApprovalAction struct {
	Action  string `json:"action"`
	TaskID  uint64 `json:"task_id"`
	Version int32  `json:"version"`
}

// ApprovalSnapshots reads the parked proposal an approval card was rendered
// from. *execute.AgentExecutor satisfies this via ApprovalSnapshot.
type ApprovalSnapshots interface {
	ApprovalSnapshot(ctx context.Context, taskID uint64) (execute.ApprovalNotification, error)
}

// CardRenderer re-renders an approval card in its decided state. *Notifier
// satisfies this via ResolvedCard.
type CardRenderer interface {
	ResolvedCard(notice execute.ApprovalNotification, outcome string) (json.RawMessage, error)
}

type Handler struct {
	approver      Approver
	snapshots     ApprovalSnapshots
	cards         CardRenderer
	principalOpen string
	logger        *log.Logger
}

// NewRelayHandler builds the approval processor behind the authenticated
// localhost relay. Jarvis never opens a Feishu event connection itself.
func NewRelayHandler(approver Approver, snapshots ApprovalSnapshots, cards CardRenderer, principalOpenID string, logger *log.Logger) (*Handler, error) {
	if approver == nil {
		return nil, fmt.Errorf("card approval approver is nil")
	}
	if snapshots == nil {
		return nil, fmt.Errorf("card approval snapshot reader is nil")
	}
	if cards == nil {
		return nil, fmt.Errorf("card approval card renderer is nil")
	}
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return nil, fmt.Errorf("card approval principal open_id is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("card approval logger is nil")
	}
	return &Handler{approver: approver, snapshots: snapshots, cards: cards, principalOpen: principalOpenID, logger: logger}, nil
}

// ProcessCardAction immediately lands one version-bound callback and returns
// the decided card in full. The snapshot is taken before the decision lands,
// because approving or rejecting replaces the proposal it renders from.
func (h *Handler) ProcessCardAction(ctx context.Context, event CardActionEvent) (json.RawMessage, error) {
	action, err := authorizeCardApproval(event, h.principalOpen)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", execute.ErrInvalidInput, err)
	}

	note, err := approvalNote(event.FormValue)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", execute.ErrInvalidInput, err)
	}
	notice, err := h.snapshots.ApprovalSnapshot(ctx, action.TaskID)
	if err != nil {
		return nil, fmt.Errorf("read approval snapshot task_id=%d: %w", action.TaskID, err)
	}
	switch action.Action {
	case "approve":
		if note != "" {
			updated, supplementErr := h.approver.Supplement(ctx, execute.SupplementInput{
				TaskID: action.TaskID, ExpectedVersion: action.Version, Note: note, Channel: "feishu_card",
			})
			if supplementErr != nil {
				return h.onLandFailed(notice, action, supplementErr)
			}
			action.Version = updated.Version
		}
		_, err = h.approver.KickApprove(ctx, action.TaskID, action.Version)
	case "reject":
		reason := note
		if reason == "" {
			reason = "委托人在飞书卡片上驳回"
		}
		_, err = h.approver.Reject(ctx, action.TaskID, action.Version, reason)
	default:
		return nil, fmt.Errorf("%w: unsupported card approval action %q", execute.ErrInvalidInput, action.Action)
	}
	if err != nil {
		return h.onLandFailed(notice, action, err)
	}
	if action.Action == "approve" {
		if note != "" {
			return h.resolvedCard(notice, "✅ 已确认并提交补充，正在执行。\n\n补充："+note)
		}
		return h.resolvedCard(notice, "✅ 已确认，正在执行。")
	}
	if note != "" {
		return h.resolvedCard(notice, "已驳回，不会执行。\n\n原因："+note)
	}
	return h.resolvedCard(notice, "已驳回，不会执行。")
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

func approvalNote(form map[string]any) (string, error) {
	if len(form) == 0 {
		return "", nil
	}
	for key := range form {
		if key != "approval_note" {
			return "", fmt.Errorf("card approval form contains unsupported field %q", key)
		}
	}
	raw, ok := form["approval_note"]
	if !ok || raw == nil {
		return "", nil
	}
	note, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("card approval form approval_note must be a string")
	}
	return strings.TrimSpace(note), nil
}

func (h *Handler) onLandFailed(notice execute.ApprovalNotification, action cardApprovalAction, err error) (json.RawMessage, error) {
	if errors.Is(err, execute.ErrVersionConflict) || errors.Is(err, execute.ErrInvalidTransition) {
		h.logger.Printf("job=card-approval status=info action=%s task_id=%d version=%d skipped=already-handled: %v", action.Action, action.TaskID, action.Version, err)
		return h.resolvedCard(notice, "这条审批已经处理过了，请去后台确认。")
	}
	return nil, fmt.Errorf("card approval %s task_id=%d version=%d: %w", action.Action, action.TaskID, action.Version, err)
}

// resolvedCard renders the whole card Jarvis wants shown after the decision.
// CC Connect replaces the original message with it, so the proposal copy stays
// visible as audit text without Feishu having to return the original card.
func (h *Handler) resolvedCard(notice execute.ApprovalNotification, outcome string) (json.RawMessage, error) {
	card, err := h.cards.ResolvedCard(notice, outcome)
	if err != nil {
		return nil, fmt.Errorf("render resolved approval card task_id=%d: %w", notice.TaskID, err)
	}
	return card, nil
}
