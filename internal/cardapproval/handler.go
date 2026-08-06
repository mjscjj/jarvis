// Package cardapproval bridges Feishu approval-card button clicks to the
// existing Task approve/reject actions. It is the thin, judgement-free mapping
// the design calls for: a click that reaches here already means the principal
// decided, so this package only loads the current version, calls approve or
// reject, and reflects the outcome back onto the card. It never inspects the
// task's action_type or weighs risk — which buttons a card carries is M5's call.
package cardapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"jarvis/internal/capture"
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

// CardUpdater replaces the clicked card in place using the event's delayed-update
// token. *larkcli.Client satisfies this via UpdateCard.
type CardUpdater interface {
	UpdateCard(ctx context.Context, profile, token string, card json.RawMessage) error
}

// Handler implements capture.CardActionHandler.
type Handler struct {
	tasks         ApprovalReader
	approver      Approver
	cards         CardUpdater
	principalOpen string
	profile       string
	logger        *log.Logger
	readyTimeout  time.Duration
	pollInterval  time.Duration
}

func NewHandler(tasks ApprovalReader, approver Approver, cards CardUpdater, principalOpenID, profile string, logger *log.Logger) (*Handler, error) {
	return newHandler(tasks, approver, cards, principalOpenID, profile, logger)
}

// NewRelayHandler builds the same approval processor without a card updater.
// CC Connect owns the Feishu callback response in this mode and returns the
// replacement card produced by ProcessCardAction directly to Feishu.
func NewRelayHandler(tasks ApprovalReader, approver Approver, principalOpenID string, logger *log.Logger) (*Handler, error) {
	handler, err := newHandler(tasks, approver, nil, principalOpenID, "", logger)
	if err != nil {
		return nil, err
	}
	// Feishu expects the synchronous callback response within 3 seconds and CC
	// Connect reserves 500ms for transport. A faster click can retry after M5
	// has persisted the proposal; never hold the callback for the standalone
	// consumer's longer delayed-update window.
	handler.readyTimeout = 2 * time.Second
	return handler, nil
}

func newHandler(tasks ApprovalReader, approver Approver, cards CardUpdater, principalOpenID, profile string, logger *log.Logger) (*Handler, error) {
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
	profile = strings.TrimSpace(profile)
	if cards != nil && profile == "" {
		return nil, fmt.Errorf("card approval profile is empty when card updater is configured")
	}
	if logger == nil {
		return nil, fmt.Errorf("card approval logger is nil")
	}
	return &Handler{
		tasks: tasks, approver: approver, cards: cards,
		principalOpen: principalOpenID, profile: profile, logger: logger,
		readyTimeout: 10 * time.Second, pollInterval: 100 * time.Millisecond,
	}, nil
}

// HandleCardAction lands one approve/reject click. The click is the approval, so
// the only gate is AuthorizeCardApproval's hard boundaries (principal, valid
// action, positive id). On success the card is updated to show the decision; on
// a lost race (version/state conflict) the card is nudged toward the backend.
// Unauthorized clicks never mutate the shared card. A card-update failure never fails the approval — the
// state change already committed and the backend list stays authoritative.
func (h *Handler) HandleCardAction(ctx context.Context, event capture.CardActionEvent) error {
	card, err := h.ProcessCardAction(ctx, event)
	if len(card) > 0 {
		h.updateCard(ctx, event, card)
	}
	return err
}

// ProcessCardAction mechanically lands one callback and returns the complete
// replacement card. CC Connect uses this method through the localhost relay,
// while HandleCardAction adds delayed card updating for the standalone app
// transport. Keeping the state transition in one method makes both transports
// enforce the same principal/proposal/message/version checks.
func (h *Handler) ProcessCardAction(ctx context.Context, event capture.CardActionEvent) (json.RawMessage, error) {
	action, err := capture.AuthorizeCardApproval(event, h.principalOpen)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", execute.ErrInvalidInput, err)
	}

	view, alreadyHandled, err := h.waitForProposal(ctx, action.TaskID, event.MessageID)
	if err != nil {
		return nil, fmt.Errorf("card approval load task_id=%d: %w", action.TaskID, err)
	}
	if alreadyHandled {
		return cardNoticeText("这条审批已经处理过了，请去后台查看结果。"), nil
	}
	if err := h.verifyCurrentProposal(ctx, view, event.MessageID); err != nil {
		return nil, err
	}

	switch action.Action {
	case "approve":
		if _, err := h.approver.KickApprove(ctx, action.TaskID, view.Version); err != nil {
			return h.onLandFailed(ctx, event, action.TaskID, "approve", err)
		}
		return cardNoticeText("✅ 已同意，正在处理。"), nil
	case "reject":
		if _, err := h.approver.Reject(ctx, action.TaskID, view.Version, "委托人在飞书卡片上驳回"); err != nil {
			return h.onLandFailed(ctx, event, action.TaskID, "reject", err)
		}
		return cardNoticeText("已驳回，不会执行。"), nil
	}
	return nil, fmt.Errorf("%w: unsupported card approval action %q", execute.ErrInvalidInput, action.Action)
}

// The card is sent before the agent returns its proposal. A very fast click can
// therefore arrive while the Task is still executing and before the proposal is
// persisted. An executing Task may still carry the previous proposal while its
// apply run is producing a second one, so only a card belonging to that stored
// proposal is already handled; a different card keeps waiting for the handoff.
func (h *Handler) waitForProposal(ctx context.Context, taskID uint64, messageID string) (*execute.TaskView, bool, error) {
	deadline := time.NewTimer(h.readyTimeout)
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
			return nil, false, fmt.Errorf("proposal was not persisted within %s", h.readyTimeout)
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
func (h *Handler) onLandFailed(_ context.Context, _ capture.CardActionEvent, taskID uint64, action string, err error) (json.RawMessage, error) {
	if errors.Is(err, execute.ErrVersionConflict) || errors.Is(err, execute.ErrInvalidTransition) {
		h.logger.Printf("job=card-approval status=info action=%s task_id=%d skipped=already-handled: %v", action, taskID, err)
		return cardNoticeText("这条审批可能已经处理过了，请去后台确认。"), nil
	}
	return cardNoticeText("处理没成功，请去后台重试。"), fmt.Errorf("card approval %s task_id=%d: %w", action, taskID, err)
}

func (h *Handler) updateCard(ctx context.Context, event capture.CardActionEvent, card json.RawMessage) {
	if h.cards == nil || event.Token == "" {
		return
	}
	if err := h.cards.UpdateCard(ctx, h.profile, event.Token, card); err != nil {
		// The approval already landed; a stale/exhausted token just means the
		// principal sees the old card. Log and move on, never fail the click.
		h.logger.Printf("job=card-approval status=info update-card-skipped message_id=%s: %v", event.MessageID, err)
	}
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
