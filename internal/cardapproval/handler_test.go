package cardapproval

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"jarvis/internal/execute"
)

const principalOpenID = "ou_principal"

func TestProcessCardActionApproveUsesCardVersion(t *testing.T) {
	approver := &fakeApprover{}
	handler := newTestHandler(t, approver)
	card, err := handler.ProcessCardAction(context.Background(), approvalEvent(t, "approve", 7, 4))
	if err != nil {
		t.Fatalf("ProcessCardAction() error = %v", err)
	}
	if approver.approvedTask != 7 || approver.approvedVersion != 4 {
		t.Fatalf("approve called with task=%d version=%d", approver.approvedTask, approver.approvedVersion)
	}
	if !strings.Contains(string(card), "已同意") {
		t.Fatalf("outcome card = %s", card)
	}
}

func TestProcessCardActionRejectUsesCardVersion(t *testing.T) {
	approver := &fakeApprover{}
	handler := newTestHandler(t, approver)
	if _, err := handler.ProcessCardAction(context.Background(), approvalEvent(t, "reject", 3, 9)); err != nil {
		t.Fatalf("ProcessCardAction() error = %v", err)
	}
	if approver.rejectedTask != 3 || approver.rejectedVersion != 9 || approver.rejectReason == "" {
		t.Fatalf("reject called wrong: %#v", approver)
	}
}

func TestProcessCardActionRejectsMissingVersion(t *testing.T) {
	approver := &fakeApprover{}
	handler := newTestHandler(t, approver)
	if _, err := handler.ProcessCardAction(context.Background(), approvalEvent(t, "approve", 7, 0)); !errors.Is(err, execute.ErrInvalidInput) {
		t.Fatalf("ProcessCardAction() error = %v, want ErrInvalidInput", err)
	}
	if approver.approvedTask != 0 {
		t.Fatalf("approver called for versionless card: %#v", approver)
	}
}

func TestProcessCardActionRejectsNonPrincipal(t *testing.T) {
	approver := &fakeApprover{}
	handler := newTestHandler(t, approver)
	event := approvalEvent(t, "approve", 7, 4)
	event.OperatorID = "ou_intruder"
	if _, err := handler.ProcessCardAction(context.Background(), event); !errors.Is(err, execute.ErrInvalidInput) {
		t.Fatalf("ProcessCardAction() error = %v, want ErrInvalidInput", err)
	}
}

func TestProcessCardActionLostRaceIsAlreadyHandled(t *testing.T) {
	approver := &fakeApprover{approveErr: execute.ErrVersionConflict}
	handler := newTestHandler(t, approver)
	card, err := handler.ProcessCardAction(context.Background(), approvalEvent(t, "approve", 7, 4))
	if err != nil {
		t.Fatalf("ProcessCardAction() error = %v", err)
	}
	if !strings.Contains(string(card), "已经处理") {
		t.Fatalf("outcome card = %s", card)
	}
}

func newTestHandler(t *testing.T, approver Approver) *Handler {
	t.Helper()
	handler, err := NewRelayHandler(approver, principalOpenID, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewRelayHandler() error = %v", err)
	}
	return handler
}

func approvalEvent(t *testing.T, action string, taskID uint64, version int32) CardActionEvent {
	t.Helper()
	value, err := json.Marshal(cardApprovalAction{Action: action, TaskID: taskID, Version: version})
	if err != nil {
		t.Fatalf("marshal action value: %v", err)
	}
	return CardActionEvent{
		EventID: "evt_1", OperatorID: principalOpenID, MessageID: "om_1",
		ActionTag: "button", ActionValue: string(value),
	}
}

type fakeApprover struct {
	approvedTask    uint64
	approvedVersion int32
	rejectedTask    uint64
	rejectedVersion int32
	rejectReason    string
	approveErr      error
}

func (f *fakeApprover) KickApprove(_ context.Context, taskID uint64, version int32) (*execute.ExecuteResult, error) {
	f.approvedTask, f.approvedVersion = taskID, version
	return &execute.ExecuteResult{TaskID: taskID, Status: "executing"}, f.approveErr
}

func (f *fakeApprover) Reject(_ context.Context, taskID uint64, version int32, reason string) (*execute.ExecuteResult, error) {
	f.rejectedTask, f.rejectedVersion, f.rejectReason = taskID, version, reason
	return &execute.ExecuteResult{TaskID: taskID, Status: "failed"}, nil
}
