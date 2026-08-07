// Package cardapproval maps authenticated Feishu approval-card callbacks from
// CC Connect to the existing Task approve/reject actions. It never inspects the
// task's action_type or weighs risk; which buttons a card carries is M5's call.
package cardapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"jarvis/internal/execute"
)

// ApprovalReader loads the current proposal and its execution run. The callback
// message must be one of that run's declared effects; this binds a card to the
// exact proposal it was sent for without adding a new persistence shape.
type ApprovalReader interface {
	GetTask(context.Context, uint64) (*execute.TaskView, error)
	ListRuns(context.Context, uint64) (*execute.RunList, error)
}

// Approver lands or declines an awaiting_approval Task. *execute.AgentExecutor
// satisfies this via KickApprove/Reject.
type Approver interface {
	KickApprove(ctx context.Context, taskID uint64, expectedVersion int32) (*execute.ExecuteResult, error)
	Reject(ctx context.Context, taskID uint64, expectedVersion int32, reason string) (*execute.ExecuteResult, error)
}

// CardActionEvent is the strict relay payload needed to bind one Feishu click
// to the current proposal. CC Connect owns the Feishu callback connection.
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
	Action string `json:"action"`
	TaskID uint64 `json:"task_id"`
}

type Handler struct {
	tasks                ApprovalReader
	approver             Approver
	principalOpen        string
	logger               *log.Logger
	readyTimeout         time.Duration
	deferredReadyTimeout time.Duration
	pollInterval         time.Duration
}

var errProposalNotReady = errors.New("approval proposal is not ready")

// NewRelayHandler builds the approval processor behind the authenticated
// localhost relay. Jarvis never opens a Feishu event connection itself.
func NewRelayHandler(tasks ApprovalReader, approver Approver, principalOpenID string, logger *log.Logger) (*Handler, error) {
	if tasks == nil {
		return nil, fmt.Errorf("card approval task reader is nil")
	}
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
	return &Handler{
		tasks: tasks, approver: approver, principalOpen: principalOpenID, logger: logger,
		readyTimeout: 250 * time.Millisecond, deferredReadyTimeout: 30 * time.Second, pollInterval: 100 * time.Millisecond,
	}, nil
}

// ProcessCardAction mechanically lands one callback and returns the complete
// replacement card for CC Connect to return synchronously to Feishu.
func (h *Handler) ProcessCardAction(ctx context.Context, event CardActionEvent) (json.RawMessage, error) {
	action, err := authorizeCardApproval(event, h.principalOpen)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", execute.ErrInvalidInput, err)
	}

	view, alreadyHandled, err := h.waitForProposal(ctx, action.TaskID, event.MessageID, h.readyTimeout)
	if errors.Is(err, errProposalNotReady) {
		h.deferCardApproval(event, action)
		if action.Action == "approve" {
			return cardNoticeText("✅ 已收到同意，正在处理。"), nil
		}
		return cardNoticeText("已收到拒绝，正在处理。"), nil
	}
	if err != nil {
		return nil, fmt.Errorf("card approval load task_id=%d: %w", action.TaskID, err)
	}
	if alreadyHandled {
		return cardNoticeText("这条审批已经处理过了，请去后台查看结果。"), nil
	}
	if err := h.verifyCurrentProposal(ctx, view, event.MessageID); err != nil {
		return nil, err
	}

	if err := h.landCardApproval(ctx, action, view.Version); err != nil {
		return h.onLandFailed(ctx, event, action.TaskID, action.Action, err)
	}
	if action.Action == "approve" {
		return cardNoticeText("✅ 已同意，正在处理。"), nil
	}
	return cardNoticeText("已驳回，不会执行。"), nil
}

func (h *Handler) landCardApproval(ctx context.Context, action cardApprovalAction, version int32) error {
	switch action.Action {
	case "approve":
		_, err := h.approver.KickApprove(ctx, action.TaskID, version)
		return err
	case "reject":
		_, err := h.approver.Reject(ctx, action.TaskID, version, "委托人在飞书卡片上驳回")
		return err
	default:
		return fmt.Errorf("%w: unsupported card approval action %q", execute.ErrInvalidInput, action.Action)
	}
}

func (h *Handler) deferCardApproval(event CardActionEvent, action cardApprovalAction) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), h.deferredReadyTimeout)
		defer cancel()
		view, alreadyHandled, err := h.waitForProposal(ctx, action.TaskID, event.MessageID, h.deferredReadyTimeout)
		if err != nil {
			h.logger.Printf("job=card-approval status=error action=%s task_id=%d deferred=true error=%v", action.Action, action.TaskID, err)
			return
		}
		if alreadyHandled {
			h.logger.Printf("job=card-approval status=info action=%s task_id=%d deferred=true skipped=already-handled", action.Action, action.TaskID)
			return
		}
		if err := h.verifyCurrentProposal(ctx, view, event.MessageID); err != nil {
			h.logger.Printf("job=card-approval status=error action=%s task_id=%d deferred=true error=%v", action.Action, action.TaskID, err)
			return
		}
		if err := h.landCardApproval(ctx, action, view.Version); err != nil {
			if errors.Is(err, execute.ErrVersionConflict) || errors.Is(err, execute.ErrInvalidTransition) {
				h.logger.Printf("job=card-approval status=info action=%s task_id=%d deferred=true skipped=already-handled: %v", action.Action, action.TaskID, err)
				return
			}
			h.logger.Printf("job=card-approval status=error action=%s task_id=%d deferred=true error=%v", action.Action, action.TaskID, err)
			return
		}
		h.logger.Printf("job=card-approval status=ok action=%s task_id=%d deferred=true", action.Action, action.TaskID)
	}()
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
	switch action.Action {
	case "approve", "reject":
	default:
		return cardApprovalAction{}, fmt.Errorf("card action %q is not approve or reject", action.Action)
	}
	if action.TaskID == 0 {
		return cardApprovalAction{}, fmt.Errorf("card action task_id must be positive")
	}
	return action, nil
}

// The card is sent before the agent returns its proposal. A very fast click can
// therefore arrive while the Task is still executing and before the proposal is
// persisted. An executing Task may still carry the previous proposal while its
// apply run is producing a second one, so only a card belonging to that stored
// proposal is already handled; a different card keeps waiting for the handoff.
func (h *Handler) waitForProposal(ctx context.Context, taskID uint64, messageID string, timeout time.Duration) (*execute.TaskView, bool, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	for {
		task, err := h.tasks.GetTask(ctx, taskID)
		if err != nil {
			return nil, false, err
		}
		switch task.Status {
		case "awaiting_approval":
			return task, false, nil
		case "executing":
			if _, err := proposalSourceRunID(task.ExecutionResult); err == nil {
				matches, err := h.proposalHasMessageID(ctx, task, messageID)
				if err != nil {
					return nil, false, err
				}
				if matches {
					return task, true, nil
				}
			}
		default:
			return task, true, nil
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-deadline.C:
			return nil, false, fmt.Errorf("%w within %s", errProposalNotReady, timeout)
		case <-ticker.C:
		}
	}
}

func (h *Handler) verifyCurrentProposal(ctx context.Context, task *execute.TaskView, messageID string) error {
	if task == nil {
		return fmt.Errorf("%w: approval task is nil", execute.ErrInvalidInput)
	}
	if task.Status != "awaiting_approval" {
		return fmt.Errorf("%w: task_id=%d status=%q is not awaiting approval", execute.ErrInvalidTransition, task.ID, task.Status)
	}
	matches, err := h.proposalHasMessageID(ctx, task, messageID)
	if err != nil {
		return err
	}
	if !matches {
		sourceRunID, _ := proposalSourceRunID(task.ExecutionResult)
		return fmt.Errorf("%w: card message_id=%q was not sent by source_run_id=%d", execute.ErrInvalidInput, messageID, sourceRunID)
	}
	return nil
}

func (h *Handler) proposalHasMessageID(ctx context.Context, task *execute.TaskView, messageID string) (bool, error) {
	sourceRunID, err := proposalSourceRunID(task.ExecutionResult)
	if err != nil {
		return false, fmt.Errorf("card approval task_id=%d: %w", task.ID, err)
	}
	runs, err := h.tasks.ListRuns(ctx, task.ID)
	if err != nil {
		return false, fmt.Errorf("card approval list runs task_id=%d: %w", task.ID, err)
	}
	for _, run := range runs.Items {
		if run.ID != sourceRunID {
			continue
		}
		return runEffectHasMessageID(run.Effects, messageID), nil
	}
	return false, fmt.Errorf("%w: source_run_id=%d not found for task_id=%d", execute.ErrInvalidInput, sourceRunID, task.ID)
}

func proposalSourceRunID(raw json.RawMessage) (uint64, error) {
	var stored struct {
		Stage       string `json:"stage"`
		SourceRunID uint64 `json:"source_run_id"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return 0, fmt.Errorf("decode pending proposal: %w", err)
	}
	if stored.Stage != "proposal" || stored.SourceRunID == 0 {
		return 0, fmt.Errorf("pending proposal has stage=%q source_run_id=%d", stored.Stage, stored.SourceRunID)
	}
	return stored.SourceRunID, nil
}

func runEffectHasMessageID(raw json.RawMessage, messageID string) bool {
	if messageID == "" {
		return false
	}
	var effects []map[string]json.RawMessage
	if json.Unmarshal(raw, &effects) != nil {
		return false
	}
	for _, effect := range effects {
		if rawMessageID(effect["message_id"]) == messageID {
			return true
		}
		extra := effect["extra"]
		var encoded string
		if json.Unmarshal(extra, &encoded) == nil {
			extra = json.RawMessage(encoded)
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(extra, &nested) == nil && rawMessageID(nested["message_id"]) == messageID {
			return true
		}
	}
	return false
}

func rawMessageID(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// onLandFailed distinguishes a lost race (someone already handled it, or the
// task moved on) from a real error. Either way the card points the principal at
// the backend; only unexpected errors propagate to the consumer log.
func (h *Handler) onLandFailed(_ context.Context, _ CardActionEvent, taskID uint64, action string, err error) (json.RawMessage, error) {
	if errors.Is(err, execute.ErrVersionConflict) || errors.Is(err, execute.ErrInvalidTransition) {
		h.logger.Printf("job=card-approval status=info action=%s task_id=%d skipped=already-handled: %v", action, taskID, err)
		return cardNoticeText("这条审批可能已经处理过了，请去后台确认。"), nil
	}
	return cardNoticeText("处理没成功，请去后台重试。"), fmt.Errorf("card approval %s task_id=%d: %w", action, taskID, err)
}

// cardNoticeText builds a minimal Card 2.0 body that states the outcome. It is a
// plain replacement of the original card — the buttons are gone, so no further
// click is possible on a resolved approval.
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
