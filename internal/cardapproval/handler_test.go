package cardapproval

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"testing"

	"jarvis/internal/capture"
	"jarvis/internal/execute"
)

const principalOpenID = "ou_principal"

func TestHandleCardActionApproveLandsAndUpdatesCard(t *testing.T) {
	tasks := newFakeTasks(4)
	approver := &fakeApprover{}
	cards := &fakeCards{}
	handler := newTestHandler(t, tasks, approver, cards)

	err := handler.HandleCardAction(context.Background(), approveEvent(t, "approve", 7))
	if err != nil {
		t.Fatalf("HandleCardAction() error = %v", err)
	}
	if approver.approvedTask != 7 || approver.approvedVersion != 4 {
		t.Fatalf("approve called with task=%d version=%d", approver.approvedTask, approver.approvedVersion)
	}
	if approver.rejectedTask != 0 {
		t.Fatalf("reject unexpectedly called: %#v", approver)
	}
	if cards.updates != 1 {
		t.Fatalf("card update count = %d, want 1", cards.updates)
	}
	if cards.profile != "cli_approval" {
		t.Fatalf("card update profile = %q", cards.profile)
	}
}

func TestHandleCardActionRejectLandsWithReason(t *testing.T) {
	tasks := newFakeTasks(9)
	approver := &fakeApprover{}
	cards := &fakeCards{}
	handler := newTestHandler(t, tasks, approver, cards)

	if err := handler.HandleCardAction(context.Background(), approveEvent(t, "reject", 3)); err != nil {
		t.Fatalf("HandleCardAction() error = %v", err)
	}
	if approver.rejectedTask != 3 || approver.rejectedVersion != 9 || approver.rejectReason == "" {
		t.Fatalf("reject called wrong: %#v", approver)
	}
}

func TestHandleCardActionNonPrincipalIsRejected(t *testing.T) {
	tasks := newFakeTasks(1)
	approver := &fakeApprover{}
	cards := &fakeCards{}
	handler := newTestHandler(t, tasks, approver, cards)

	event := approveEvent(t, "approve", 7)
	event.OperatorID = "ou_intruder"
	if err := handler.HandleCardAction(context.Background(), event); !errors.Is(err, execute.ErrInvalidInput) {
		t.Fatalf("HandleCardAction() error = %v, want ErrInvalidInput", err)
	}
	if approver.approvedTask != 0 || approver.rejectedTask != 0 {
		t.Fatalf("approver touched for non-principal: %#v", approver)
	}
	if cards.updates != 0 {
		t.Fatalf("non-principal must not mutate the shared card, updates = %d", cards.updates)
	}
}

func TestHandleCardActionAlreadyHandledIsSkippedNotFailed(t *testing.T) {
	tasks := newFakeTasks(2)
	approver := &fakeApprover{approveErr: execute.ErrVersionConflict}
	cards := &fakeCards{}
	handler := newTestHandler(t, tasks, approver, cards)

	if err := handler.HandleCardAction(context.Background(), approveEvent(t, "approve", 7)); err != nil {
		t.Fatalf("HandleCardAction() on lost race should be nil, got %v", err)
	}
	if cards.updates != 1 {
		t.Fatalf("expected an already-handled notice, updates = %d", cards.updates)
	}
}

func TestHandleCardActionCardUpdateFailureDoesNotUndoApproval(t *testing.T) {
	tasks := newFakeTasks(4)
	approver := &fakeApprover{}
	cards := &fakeCards{lastErr: errors.New("expired token")}
	handler := newTestHandler(t, tasks, approver, cards)

	if err := handler.HandleCardAction(context.Background(), approveEvent(t, "approve", 7)); err != nil {
		t.Fatalf("HandleCardAction() update failure = %v", err)
	}
	if approver.approvedTask != 7 {
		t.Fatalf("approval was not landed: %#v", approver)
	}
}

func TestHandleCardActionFastRepeatIsReportedAsAlreadyHandled(t *testing.T) {
	tasks := newFakeTasks(4)
	tasks.status = "executing"
	approver := &fakeApprover{}
	cards := &fakeCards{}
	handler := newTestHandler(t, tasks, approver, cards)

	if err := handler.HandleCardAction(context.Background(), approveEvent(t, "approve", 7)); err != nil {
		t.Fatalf("HandleCardAction() error = %v", err)
	}
	if approver.approvedTask != 0 {
		t.Fatalf("fast click approved task: %#v", approver)
	}
	if cards.updates != 1 {
		t.Fatalf("fast repeat should replace the card, updates = %d", cards.updates)
	}
}

func TestHandleCardActionWaitsForSecondProposalInsteadOfClosingNewCard(t *testing.T) {
	tasks := &secondProposalTasks{}
	approver := &fakeApprover{}
	cards := &fakeCards{}
	handler := newTestHandler(t, tasks, approver, cards)

	if err := handler.HandleCardAction(context.Background(), approveEvent(t, "approve", 7)); err != nil {
		t.Fatalf("HandleCardAction() error = %v", err)
	}
	if approver.approvedTask != 7 || approver.approvedVersion != 6 {
		t.Fatalf("second proposal approval = task=%d version=%d", approver.approvedTask, approver.approvedVersion)
	}
	if cards.updates != 1 {
		t.Fatalf("second proposal should replace the card after approval, updates = %d", cards.updates)
	}
}

func TestHandleCardActionRejectsStaleCardMessage(t *testing.T) {
	tasks := newFakeTasks(4)
	tasks.messageID = "om_new_proposal"
	approver := &fakeApprover{}
	handler := newTestHandler(t, tasks, approver, &fakeCards{})

	if err := handler.HandleCardAction(context.Background(), approveEvent(t, "approve", 7)); !errors.Is(err, execute.ErrInvalidInput) {
		t.Fatalf("HandleCardAction() error = %v, want ErrInvalidInput", err)
	}
	if approver.approvedTask != 0 {
		t.Fatalf("stale card approved task: %#v", approver)
	}
}

func newTestHandler(t *testing.T, tasks ApprovalReader, approver Approver, cards CardUpdater) *Handler {
	t.Helper()
	handler, err := NewHandler(tasks, approver, cards, principalOpenID, "cli_approval", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func approveEvent(t *testing.T, action string, taskID uint64) capture.CardActionEvent {
	t.Helper()
	value, err := json.Marshal(capture.CardApprovalAction{Action: action, TaskID: taskID})
	if err != nil {
		t.Fatalf("marshal action value: %v", err)
	}
	return capture.CardActionEvent{
		Type:        "card.action.trigger",
		EventID:     "evt_1",
		OperatorID:  principalOpenID,
		MessageID:   "om_1",
		Token:       "tok_1",
		ActionTag:   "button",
		ActionValue: string(value),
	}
}

type fakeTasks struct {
	version   int32
	status    string
	sourceRun uint64
	messageID string
	err       error
}

func newFakeTasks(version int32) *fakeTasks {
	return &fakeTasks{version: version, status: "awaiting_approval", sourceRun: 51, messageID: "om_1"}
}

func (f *fakeTasks) GetTask(_ context.Context, id uint64) (*execute.TaskView, error) {
	if f.err != nil {
		return nil, f.err
	}
	result, _ := json.Marshal(map[string]any{"stage": "proposal", "source_run_id": f.sourceRun})
	return &execute.TaskView{ID: id, Status: f.status, Version: f.version, ExecutionResult: result}, nil
}

func (f *fakeTasks) ListRuns(_ context.Context, taskID uint64) (*execute.RunList, error) {
	if f.err != nil {
		return nil, f.err
	}
	effects, _ := json.Marshal([]map[string]any{{"kind": "feishu_message", "message_id": f.messageID}})
	return &execute.RunList{Items: []execute.RunView{{ID: f.sourceRun, TaskID: taskID, Effects: effects}}}, nil
}

type secondProposalTasks struct {
	loads int
}

func (f *secondProposalTasks) GetTask(_ context.Context, id uint64) (*execute.TaskView, error) {
	f.loads++
	status, version, sourceRun := "executing", int32(5), uint64(51)
	if f.loads > 1 {
		status, version, sourceRun = "awaiting_approval", 6, 52
	}
	result, _ := json.Marshal(map[string]any{"stage": "proposal", "source_run_id": sourceRun})
	return &execute.TaskView{ID: id, Status: status, Version: version, ExecutionResult: result}, nil
}

func (f *secondProposalTasks) ListRuns(_ context.Context, taskID uint64) (*execute.RunList, error) {
	oldEffects, _ := json.Marshal([]map[string]any{{"kind": "feishu_message", "message_id": "om_old"}})
	newEffects, _ := json.Marshal([]map[string]any{{"kind": "feishu_message", "message_id": "om_1"}})
	return &execute.RunList{Items: []execute.RunView{
		{ID: 52, TaskID: taskID, Effects: newEffects},
		{ID: 51, TaskID: taskID, Effects: oldEffects},
	}}, nil
}

type fakeApprover struct {
	approvedTask    uint64
	approvedVersion int32
	rejectedTask    uint64
	rejectedVersion int32
	rejectReason    string
	approveErr      error
	rejectErr       error
}

func (f *fakeApprover) KickApprove(_ context.Context, taskID uint64, version int32) (*execute.ExecuteResult, error) {
	f.approvedTask = taskID
	f.approvedVersion = version
	if f.approveErr != nil {
		return nil, f.approveErr
	}
	return &execute.ExecuteResult{}, nil
}

func (f *fakeApprover) Reject(_ context.Context, taskID uint64, version int32, reason string) (*execute.ExecuteResult, error) {
	f.rejectedTask = taskID
	f.rejectedVersion = version
	f.rejectReason = reason
	if f.rejectErr != nil {
		return nil, f.rejectErr
	}
	return &execute.ExecuteResult{}, nil
}

type fakeCards struct {
	updates int
	profile string
	lastErr error
}

func (f *fakeCards) UpdateCard(_ context.Context, profile, _ string, _ json.RawMessage) error {
	f.updates++
	f.profile = profile
	return f.lastErr
}
