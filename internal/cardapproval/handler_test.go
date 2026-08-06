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

func TestProcessCardActionApproveLandsAndReturnsCard(t *testing.T) {
	tasks := newFakeTasks(4)
	approver := &fakeApprover{}
	handler := newTestHandler(t, tasks, approver)

	card, err := handler.ProcessCardAction(context.Background(), approveEvent(t, "approve", 7))
	if err != nil {
		t.Fatalf("ProcessCardAction() error = %v", err)
	}
	if approver.approvedTask != 7 || approver.approvedVersion != 4 {
		t.Fatalf("approve called with task=%d version=%d", approver.approvedTask, approver.approvedVersion)
	}
	if approver.rejectedTask != 0 {
		t.Fatalf("reject unexpectedly called: %#v", approver)
	}
	if !strings.Contains(string(card), "已同意") {
		t.Fatalf("replacement card = %s", card)
	}
}

func TestProcessCardActionRejectLandsWithReason(t *testing.T) {
	tasks := newFakeTasks(9)
	approver := &fakeApprover{}
	handler := newTestHandler(t, tasks, approver)

	if _, err := handler.ProcessCardAction(context.Background(), approveEvent(t, "reject", 3)); err != nil {
		t.Fatalf("ProcessCardAction() error = %v", err)
	}
	if approver.rejectedTask != 3 || approver.rejectedVersion != 9 || approver.rejectReason == "" {
		t.Fatalf("reject called wrong: %#v", approver)
	}
}

func TestProcessCardActionNonPrincipalIsRejected(t *testing.T) {
	tasks := newFakeTasks(1)
	approver := &fakeApprover{}
	handler := newTestHandler(t, tasks, approver)

	event := approveEvent(t, "approve", 7)
	event.OperatorID = "ou_intruder"
	if _, err := handler.ProcessCardAction(context.Background(), event); !errors.Is(err, execute.ErrInvalidInput) {
		t.Fatalf("ProcessCardAction() error = %v, want ErrInvalidInput", err)
	}
	if approver.approvedTask != 0 || approver.rejectedTask != 0 {
		t.Fatalf("approver touched for non-principal: %#v", approver)
	}
}

func TestProcessCardActionRejectsInvalidCallbackControls(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CardActionEvent)
	}{
		{name: "non button", mutate: func(event *CardActionEvent) { event.ActionTag = "checker" }},
		{name: "form submit", mutate: func(event *CardActionEvent) { event.FormValue = `{"reason":"x"}` }},
		{name: "empty value", mutate: func(event *CardActionEvent) { event.ActionValue = "" }},
		{name: "malformed value", mutate: func(event *CardActionEvent) { event.ActionValue = `{"action":` }},
		{name: "unknown action", mutate: func(event *CardActionEvent) {
			event.ActionValue = `{"action":"delete","task_id":7}`
		}},
		{name: "zero task id", mutate: func(event *CardActionEvent) {
			event.ActionValue = `{"action":"approve","task_id":0}`
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			tasks := newFakeTasks(1)
			approver := &fakeApprover{}
			handler := newTestHandler(t, tasks, approver)
			event := approveEvent(t, "approve", 7)
			testCase.mutate(&event)

			if _, err := handler.ProcessCardAction(context.Background(), event); !errors.Is(err, execute.ErrInvalidInput) {
				t.Fatalf("ProcessCardAction() error = %v, want ErrInvalidInput", err)
			}
			if approver.approvedTask != 0 || approver.rejectedTask != 0 {
				t.Fatalf("approver touched for invalid callback: %#v", approver)
			}
		})
	}
}

func TestProcessCardActionAlreadyHandledIsSkippedNotFailed(t *testing.T) {
	tasks := newFakeTasks(2)
	approver := &fakeApprover{approveErr: execute.ErrVersionConflict}
	handler := newTestHandler(t, tasks, approver)

	card, err := handler.ProcessCardAction(context.Background(), approveEvent(t, "approve", 7))
	if err != nil {
		t.Fatalf("ProcessCardAction() on lost race should be nil, got %v", err)
	}
	if !strings.Contains(string(card), "已经处理") {
		t.Fatalf("replacement card = %s", card)
	}
}

func TestProcessCardActionFastRepeatIsReportedAsAlreadyHandled(t *testing.T) {
	tasks := newFakeTasks(4)
	tasks.status = "executing"
	approver := &fakeApprover{}
	handler := newTestHandler(t, tasks, approver)

	card, err := handler.ProcessCardAction(context.Background(), approveEvent(t, "approve", 7))
	if err != nil {
		t.Fatalf("ProcessCardAction() error = %v", err)
	}
	if approver.approvedTask != 0 {
		t.Fatalf("fast click approved task: %#v", approver)
	}
	if !strings.Contains(string(card), "已经处理") {
		t.Fatalf("replacement card = %s", card)
	}
}

func TestProcessCardActionWaitsForSecondProposalInsteadOfClosingNewCard(t *testing.T) {
	tasks := &secondProposalTasks{}
	approver := &fakeApprover{}
	handler := newTestHandler(t, tasks, approver)

	if _, err := handler.ProcessCardAction(context.Background(), approveEvent(t, "approve", 7)); err != nil {
		t.Fatalf("ProcessCardAction() error = %v", err)
	}
	if approver.approvedTask != 7 || approver.approvedVersion != 6 {
		t.Fatalf("second proposal approval = task=%d version=%d", approver.approvedTask, approver.approvedVersion)
	}
}

func TestProcessCardActionRejectsStaleCardMessage(t *testing.T) {
	tasks := newFakeTasks(4)
	tasks.messageID = "om_new_proposal"
	approver := &fakeApprover{}
	handler := newTestHandler(t, tasks, approver)

	if _, err := handler.ProcessCardAction(context.Background(), approveEvent(t, "approve", 7)); !errors.Is(err, execute.ErrInvalidInput) {
		t.Fatalf("ProcessCardAction() error = %v, want ErrInvalidInput", err)
	}
	if approver.approvedTask != 0 {
		t.Fatalf("stale card approved task: %#v", approver)
	}
}

func newTestHandler(t *testing.T, tasks ApprovalReader, approver Approver) *Handler {
	t.Helper()
	handler, err := NewRelayHandler(tasks, approver, principalOpenID, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewRelayHandler() error = %v", err)
	}
	return handler
}

func approveEvent(t *testing.T, action string, taskID uint64) CardActionEvent {
	t.Helper()
	value, err := json.Marshal(cardApprovalAction{Action: action, TaskID: taskID})
	if err != nil {
		t.Fatalf("marshal action value: %v", err)
	}
	return CardActionEvent{
		EventID:     "evt_1",
		OperatorID:  principalOpenID,
		MessageID:   "om_1",
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
