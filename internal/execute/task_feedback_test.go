package execute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type unavailableTaskFeedback struct {
	reactionCalls int
	replyCalls    int
	updateCalls   int
}

type successfulTaskFeedback struct {
	reactionCalls int
	replyCalls    int
	updateCalls   int
}

func (f *successfulTaskFeedback) AddProcessingReaction(context.Context, TaskFeedbackTarget) (*TaskFeedbackReaction, error) {
	f.reactionCalls++
	return &TaskFeedbackReaction{ReactionID: "reaction_on_it"}, nil
}

func (f *successfulTaskFeedback) ReplyResult(_ context.Context, _ uint64, _ TaskFeedbackTarget, _ string, userMessage string) (*TaskFeedbackDelivery, error) {
	f.replyCalls++
	return &TaskFeedbackDelivery{MessageID: "om_result", Preview: userMessage}, nil
}

func (f *successfulTaskFeedback) UpdateResult(_ context.Context, _ string, _ string, userMessage string) (string, error) {
	f.updateCalls++
	return userMessage, nil
}

func (f *unavailableTaskFeedback) AddProcessingReaction(context.Context, TaskFeedbackTarget) (*TaskFeedbackReaction, error) {
	f.reactionCalls++
	return nil, errors.New("230002 Bot/User can NOT be out of the chat")
}

func (f *unavailableTaskFeedback) ReplyResult(context.Context, uint64, TaskFeedbackTarget, string, string) (*TaskFeedbackDelivery, error) {
	f.replyCalls++
	return nil, errors.New("230002 Bot/User can NOT be out of the chat")
}

func (f *unavailableTaskFeedback) UpdateResult(context.Context, string, string, string) (string, error) {
	f.updateCalls++
	return "", errors.New("230002 Bot/User can NOT be out of the chat")
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
			action_type TEXT NOT NULL,
			stage TEXT NOT NULL,
			sandbox TEXT NOT NULL,
			status TEXT NOT NULL,
			prompt TEXT NOT NULL,
			codex_session_id TEXT,
			summary TEXT,
			output JSON,
			effects JSON,
			error_detail TEXT,
			input_tokens INTEGER,
			cached_input_tokens INTEGER,
			output_tokens INTEGER,
			reasoning_output_tokens INTEGER,
			repo_path TEXT,
			started_at DATETIME NOT NULL,
			finished_at DATETIME,
			duration_ms INTEGER,
			created_at DATETIME
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
	if feedback.reactionCalls != 1 || feedback.replyCalls != 0 || feedback.updateCalls != 0 {
		t.Fatalf("feedback calls = reaction:%d reply:%d update:%d", feedback.reactionCalls, feedback.replyCalls, feedback.updateCalls)
	}
	if run.Status != "running" || len(run.Effects) != 0 {
		t.Fatalf("run changed by unavailable feedback: status=%s effects=%s", run.Status, run.Effects)
	}
}

func TestTryNotifyTaskFeedbackAttemptsResultAfterReactionWasUnavailable(t *testing.T) {
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

	executor.tryNotifyTaskFeedback(t.Context(), &ExecuteResult{TaskID: 453, Status: "done", UserMessage: "完成"})
	if feedback.replyCalls != 1 || feedback.updateCalls != 0 {
		t.Fatalf("result feedback calls = reply:%d update:%d", feedback.replyCalls, feedback.updateCalls)
	}
}

func TestTaskFeedbackKeepsOnItAndReusesOneResultReply(t *testing.T) {
	raw, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		Principal:       &contextsnap.Principal{OpenID: "ou_me", Name: "我"},
		Messages: []contextsnap.Message{{
			MessageID: "om_source", ChatMode: "group", CreateTime: 20,
		}},
	}).Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	store := newTaskFeedbackTestStore(t)
	if err := store.db.Exec("INSERT INTO task(id, background) VALUES (?, ?)", 453, raw).Error; err != nil {
		t.Fatalf("insert Task: %v", err)
	}
	run := &domain.ExecutionRun{
		TaskID: 453, ActionType: "investigate", Stage: "execute", Sandbox: "danger-full-access",
		Status: "running", Prompt: "test", StartedAt: time.Now().UTC(),
	}
	if err := store.SaveRun(t.Context(), run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	feedback := &successfulTaskFeedback{}
	executor := &AgentExecutor{store: store, feedback: feedback}
	task := &domain.Task{ID: 453, Background: datatypes.JSON(raw)}

	if err := executor.startTaskFeedback(t.Context(), task, run); err != nil {
		t.Fatalf("startTaskFeedback() error = %v", err)
	}
	if err := executor.notifyTaskFeedback(t.Context(), &ExecuteResult{
		TaskID: 453, RunID: run.ID, Status: "waiting", UserMessage: "等待外部结果",
	}); err != nil {
		t.Fatalf("first notifyTaskFeedback() error = %v", err)
	}
	if err := executor.notifyTaskFeedback(t.Context(), &ExecuteResult{
		TaskID: 453, RunID: run.ID, Status: "done", UserMessage: "已经完成",
	}); err != nil {
		t.Fatalf("second notifyTaskFeedback() error = %v", err)
	}
	if feedback.reactionCalls != 1 || feedback.replyCalls != 1 || feedback.updateCalls != 1 {
		t.Fatalf("feedback calls = reaction:%d reply:%d update:%d", feedback.reactionCalls, feedback.replyCalls, feedback.updateCalls)
	}

	stored, err := store.LoadRun(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	var effects []map[string]any
	if err := json.Unmarshal(stored.Effects, &effects); err != nil {
		t.Fatalf("decode effects: %v", err)
	}
	if len(effects) != 3 || effects[0]["purpose"] != "task_processing" || effects[1]["purpose"] != "task_result" || effects[2]["purpose"] != "task_result_update" {
		t.Fatalf("effects = %#v", effects)
	}
	if effects[0]["emoji_type"] != "OnIt" || effects[0]["operation"] != "add" {
		t.Fatalf("processing reaction effect = %#v", effects[0])
	}
	for _, effect := range effects {
		if effect["operation"] == "delete" {
			t.Fatalf("OnIt reaction was deleted: %#v", effects)
		}
	}
}

func TestRecordAgentVerdictPreservesRuntimeFeedbackEffect(t *testing.T) {
	run := &domain.ExecutionRun{Effects: datatypes.JSON(json.RawMessage(`[{"kind":"feishu_reaction","purpose":"task_processing","reaction_id":"reaction_on_it"}]`))}
	if err := recordAgentVerdict(run, "done", &codexResult{}, []codexEffect{{Kind: "file", Title: "产物"}}); err != nil {
		t.Fatalf("recordAgentVerdict() error = %v", err)
	}
	var effects []map[string]any
	if err := json.Unmarshal(run.Effects, &effects); err != nil {
		t.Fatalf("decode effects: %v", err)
	}
	if len(effects) != 2 || effects[0]["reaction_id"] != "reaction_on_it" || effects[1]["kind"] != "file" {
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
