package execute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"jarvis/internal/contextsnap"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type unavailableTaskFeedback struct {
	replyCalls  int
	updateCalls int
}

func (f *unavailableTaskFeedback) ReplyProcessing(context.Context, uint64, TaskFeedbackTarget) (*TaskFeedbackDelivery, error) {
	f.replyCalls++
	return nil, errors.New("230002 Bot/User can NOT be out of the chat")
}

func (f *unavailableTaskFeedback) Update(context.Context, string, string, string) error {
	f.updateCalls++
	return errors.New("230002 Bot/User can NOT be out of the chat")
}

func newTaskFeedbackTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE task (id INTEGER PRIMARY KEY, background JSON NOT NULL)`,
		`CREATE TABLE execution_run (
			id INTEGER PRIMARY KEY,
			task_id INTEGER NOT NULL,
			status TEXT,
			effects JSON,
			started_at DATETIME
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create Task feedback test table: %v", err)
		}
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store
}

func TestTaskFeedbackTargetUsesNewestFeishuEvidenceAndThread(t *testing.T) {
	raw, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		Principal:       &contextsnap.Principal{OpenID: "ou_me", Name: "我"},
		Messages: []contextsnap.Message{
			{MessageID: "meeting:1", CreateTime: 30},
			{MessageID: "om_old", CreateTime: 10},
			{MessageID: "om_new", ThreadID: "omt_topic", CreateTime: 20},
		},
	}).Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := taskFeedbackTarget(raw)
	if err != nil || got.SourceMessageID != "om_new" || !got.ReplyInThread {
		t.Fatalf("taskFeedbackTarget() = %#v, %v", got, err)
	}
}

func TestTaskFeedbackTargetStartsThreadForGroupRoot(t *testing.T) {
	raw, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		Principal:       &contextsnap.Principal{OpenID: "ou_me", Name: "我"},
		Messages: []contextsnap.Message{{
			MessageID: "om_group_root", ChatMode: "group", CreateTime: 20,
		}},
	}).Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := taskFeedbackTarget(raw)
	if err != nil || got.SourceMessageID != "om_group_root" || !got.ReplyInThread {
		t.Fatalf("taskFeedbackTarget() = %#v, %v", got, err)
	}
}

func TestTaskFeedbackTargetKeepsP2PInConversation(t *testing.T) {
	raw, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		Principal:       &contextsnap.Principal{OpenID: "ou_me", Name: "我"},
		Messages: []contextsnap.Message{{
			MessageID: "om_p2p_root", ChatMode: "p2p", CreateTime: 20,
		}},
	}).Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := taskFeedbackTarget(raw)
	if err != nil || got.SourceMessageID != "om_p2p_root" || got.ReplyInThread {
		t.Fatalf("taskFeedbackTarget() = %#v, %v", got, err)
	}
}

func TestStartTaskFeedbackIgnoresUnavailableSourceConversation(t *testing.T) {
	raw, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		Principal:       &contextsnap.Principal{OpenID: "ou_me", Name: "我"},
		Messages: []contextsnap.Message{{
			MessageID: "om_human_p2p", ChatMode: "p2p", CreateTime: 20,
		}},
	}).Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	feedback := &unavailableTaskFeedback{}
	executor := &AgentExecutor{store: newTaskFeedbackTestStore(t), feedback: feedback}
	run := &domain.ExecutionRun{TaskID: 453, Status: "running"}

	if err := executor.startTaskFeedback(t.Context(), &domain.Task{ID: 453, Background: datatypes.JSON(raw)}, run); err != nil {
		t.Fatalf("startTaskFeedback() error = %v, want unavailable source ignored", err)
	}
	if feedback.replyCalls != 1 || feedback.updateCalls != 0 {
		t.Fatalf("feedback calls = reply:%d update:%d", feedback.replyCalls, feedback.updateCalls)
	}
	if run.Status != "running" || len(run.Effects) != 0 {
		t.Fatalf("run changed by unavailable feedback: status=%s effects=%s", run.Status, run.Effects)
	}
}

func TestNotifyTaskFeedbackSkipsWhenInitialReplyWasUnavailable(t *testing.T) {
	raw, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		Principal:       &contextsnap.Principal{OpenID: "ou_me", Name: "我"},
		Messages: []contextsnap.Message{{
			MessageID: "om_human_p2p", ChatMode: "p2p", CreateTime: 20,
		}},
	}).Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	store := newTaskFeedbackTestStore(t)
	if err := store.db.Exec("INSERT INTO task(id, background) VALUES (?, ?)", 453, raw).Error; err != nil {
		t.Fatalf("insert Task: %v", err)
	}
	feedback := &unavailableTaskFeedback{}
	executor := &AgentExecutor{store: store, feedback: feedback}

	if err := executor.notifyTaskFeedback(t.Context(), &ExecuteResult{TaskID: 453, Status: "done", UserMessage: "完成"}); err != nil {
		t.Fatalf("notifyTaskFeedback() error = %v, want missing progress message ignored", err)
	}
	if feedback.updateCalls != 0 {
		t.Fatalf("Update() calls = %d, want 0 without an initial progress message", feedback.updateCalls)
	}
}

func TestRecordAgentVerdictPreservesRuntimeFeedbackEffect(t *testing.T) {
	run := &domain.ExecutionRun{Effects: datatypes.JSON(json.RawMessage(`[{"kind":"feishu_message","purpose":"task_progress","message_id":"om_progress"}]`))}
	if err := recordAgentVerdict(run, "done", &codexResult{}, []codexEffect{{Kind: "file", Title: "产物"}}); err != nil {
		t.Fatalf("recordAgentVerdict() error = %v", err)
	}
	var effects []map[string]any
	if err := json.Unmarshal(run.Effects, &effects); err != nil {
		t.Fatalf("decode effects: %v", err)
	}
	if len(effects) != 2 || effects[0]["message_id"] != "om_progress" || effects[1]["kind"] != "file" {
		t.Fatalf("effects = %#v", effects)
	}
}

func TestRunUserMessageNeverFallsBackToInternalSummary(t *testing.T) {
	internal := "真实目标是回答问题；消息 ID=om_internal；自查已完成"
	run := &domain.ExecutionRun{
		Summary: &internal,
		Output:  datatypes.JSON(json.RawMessage(`{"summary":"internal","user_message":"我使用 GPT-5 系列模型。"}`)),
	}
	if got := runUserMessage(run); got != "我使用 GPT-5 系列模型。" {
		t.Fatalf("runUserMessage() = %q", got)
	}
	run.Output = datatypes.JSON(json.RawMessage(`{"summary":"internal"}`))
	if got := runUserMessage(run); got != "" {
		t.Fatalf("runUserMessage() leaked internal summary: %q", got)
	}
}
