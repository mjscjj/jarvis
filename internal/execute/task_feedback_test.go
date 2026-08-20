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

type statusCheckingTaskFeedback struct {
	store         *Store
	statusAtReply string
}

func (f *successfulTaskFeedback) AddProcessingReaction(context.Context, TaskFeedbackTarget) (*TaskFeedbackReaction, error) {
	f.reactionCalls++
	return &TaskFeedbackReaction{ReactionID: "reaction_on_it"}, nil
}

func (f *successfulTaskFeedback) ReplyResult(_ context.Context, _ uint64, _ TaskFeedbackTarget, userMessage string) (*TaskFeedbackDelivery, error) {
	f.replyCalls++
	return &TaskFeedbackDelivery{MessageID: "om_result", Preview: userMessage}, nil
}

func (f *successfulTaskFeedback) UpdateResult(_ context.Context, _ string, userMessage string) (string, error) {
	f.updateCalls++
	return userMessage, nil
}

func (f *unavailableTaskFeedback) AddProcessingReaction(context.Context, TaskFeedbackTarget) (*TaskFeedbackReaction, error) {
	f.reactionCalls++
	return nil, errors.New("230002 Bot/User can NOT be out of the chat")
}

func (f *unavailableTaskFeedback) ReplyResult(context.Context, uint64, TaskFeedbackTarget, string) (*TaskFeedbackDelivery, error) {
	f.replyCalls++
	return nil, errors.New("230002 Bot/User can NOT be out of the chat")
}

func (f *unavailableTaskFeedback) UpdateResult(context.Context, string, string) (string, error) {
	f.updateCalls++
	return "", errors.New("230002 Bot/User can NOT be out of the chat")
}

func (f *statusCheckingTaskFeedback) AddProcessingReaction(context.Context, TaskFeedbackTarget) (*TaskFeedbackReaction, error) {
	return &TaskFeedbackReaction{ReactionID: "reaction_on_it"}, nil
}

func (f *statusCheckingTaskFeedback) ReplyResult(ctx context.Context, _ uint64, _ TaskFeedbackTarget, userMessage string) (*TaskFeedbackDelivery, error) {
	task, err := f.store.LoadTask(ctx, 453)
	if err != nil {
		return nil, err
	}
	f.statusAtReply = task.Status
	return &TaskFeedbackDelivery{MessageID: "om_result", Preview: userMessage}, nil
}

func (f *statusCheckingTaskFeedback) UpdateResult(context.Context, string, string) (string, error) {
	return "", errors.New("unexpected Task feedback update")
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
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY, todo_id INTEGER, title TEXT NOT NULL DEFAULT '',
			action_type TEXT NOT NULL DEFAULT '', target TEXT NOT NULL DEFAULT '',
			background JSON NOT NULL DEFAULT '{}', source_payload JSON NOT NULL DEFAULT '{}',
			source_type TEXT NOT NULL DEFAULT 'manual', source_id INTEGER, occurrence_key TEXT,
			status TEXT NOT NULL DEFAULT 'executing', execution_result JSON,
			execution_supplements JSON, summary TEXT, last_progress_at DATETIME,
			project_id INTEGER, repo_path TEXT, version INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL,
			task_version INTEGER NOT NULL, event_type TEXT NOT NULL, from_status TEXT,
			to_status TEXT NOT NULL, actor_type TEXT NOT NULL, actor_ref TEXT, run_id INTEGER,
			detail JSON, occurred_at DATETIME NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(task_id, task_version)
		)`,
		`CREATE TABLE scheduled_task (
			id INTEGER PRIMARY KEY, dispatch_kind TEXT, subject_type TEXT, subject_id INTEGER,
			source_run_id INTEGER, status TEXT, last_run_status TEXT, last_error_detail TEXT,
			last_finished_at DATETIME, updated_at DATETIME
		)`,
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

func insertTaskFeedbackRouteFixture(t *testing.T, store *Store) (*domain.Task, *domain.ExecutionRun, *codexResult) {
	t.Helper()
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
	if err := store.db.Exec(
		`INSERT INTO task(id, title, action_type, background, source_payload, status, version)
		 VALUES (?, ?, ?, ?, '{}', 'executing', 0)`,
		453, "回复来源会话", "reply_message", raw,
	).Error; err != nil {
		t.Fatalf("insert Task: %v", err)
	}
	task, err := store.LoadTask(t.Context(), 453)
	if err != nil {
		t.Fatalf("LoadTask() error = %v", err)
	}
	verdict := &codexResult{
		Outcome: "completed", Summary: "已经准备好回复", UserMessage: "答案是 42",
	}
	startedAt := time.Now().UTC().Add(-time.Second)
	finishedAt := time.Now().UTC()
	durationMs := finishedAt.Sub(startedAt).Milliseconds()
	run := &domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Stage: "execute",
		Sandbox: "danger-full-access", Status: "succeeded", Prompt: "test",
		StartedAt: startedAt, FinishedAt: &finishedAt, DurationMs: &durationMs,
	}
	if err := recordAgentVerdict(run, verdict.Summary, verdict, nil); err != nil {
		t.Fatalf("recordAgentVerdict() error = %v", err)
	}
	return task, run, verdict
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

func TestNotifyTaskFeedbackReturnsUnavailableResultError(t *testing.T) {
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

	if err := executor.notifyTaskFeedback(t.Context(), &ExecuteResult{
		TaskID: 453, Status: "done", UserMessage: "完成",
	}); err == nil {
		t.Fatal("notifyTaskFeedback() succeeded for an unavailable result conversation")
	}
	if feedback.replyCalls != 1 || feedback.updateCalls != 0 {
		t.Fatalf("result feedback calls = reply:%d update:%d", feedback.replyCalls, feedback.updateCalls)
	}
}

func TestRouteRunFailsTaskWhenResultDeliveryFails(t *testing.T) {
	store := newTaskFeedbackTestStore(t)
	task, run, verdict := insertTaskFeedbackRouteFixture(t, store)
	executor := &AgentExecutor{store: store, feedback: &unavailableTaskFeedback{}, now: time.Now}

	result, err := executor.routeRun(t.Context(), task, task.Version, run, verdict, nil)
	if err == nil {
		t.Fatal("routeRun() succeeded when the required result delivery failed")
	}
	if result == nil || result.Status != "failed" {
		t.Fatalf("routeRun() result = %#v, want failed", result)
	}
	storedTask, loadErr := store.LoadTask(t.Context(), task.ID)
	if loadErr != nil {
		t.Fatalf("LoadTask() error = %v", loadErr)
	}
	if storedTask.Status != "failed" {
		t.Fatalf("Task status = %q, want failed", storedTask.Status)
	}
	storedRun, loadErr := store.LoadRun(t.Context(), run.ID)
	if loadErr != nil {
		t.Fatalf("LoadRun() error = %v", loadErr)
	}
	if storedRun.Status != "failed" || storedRun.ErrorDetail == nil {
		t.Fatalf("ExecutionRun = %#v, want failed delivery error", storedRun)
	}
	var event domain.TaskEvent
	if loadErr := store.db.Where("task_id = ?", task.ID).Take(&event).Error; loadErr != nil {
		t.Fatalf("load Task event: %v", loadErr)
	}
	if event.EventType != "execution_failed" || event.ToStatus != "failed" {
		t.Fatalf("Task event = %#v, want execution_failed", event)
	}
}

func TestRouteRunDeliversResultBeforeMarkingTaskDone(t *testing.T) {
	store := newTaskFeedbackTestStore(t)
	task, run, verdict := insertTaskFeedbackRouteFixture(t, store)
	feedback := &statusCheckingTaskFeedback{store: store}
	executor := &AgentExecutor{store: store, feedback: feedback, now: time.Now}

	result, err := executor.routeRun(t.Context(), task, task.Version, run, verdict, nil)
	if err != nil {
		t.Fatalf("routeRun() error = %v", err)
	}
	if feedback.statusAtReply != "executing" {
		t.Fatalf("Task status during reply = %q, want executing", feedback.statusAtReply)
	}
	if result == nil || result.Status != "done" {
		t.Fatalf("routeRun() result = %#v, want done", result)
	}
	storedTask, loadErr := store.LoadTask(t.Context(), task.ID)
	if loadErr != nil {
		t.Fatalf("LoadTask() error = %v", loadErr)
	}
	if storedTask.Status != "done" {
		t.Fatalf("Task status = %q, want done", storedTask.Status)
	}
	storedRun, loadErr := store.LoadRun(t.Context(), run.ID)
	if loadErr != nil {
		t.Fatalf("LoadRun() error = %v", loadErr)
	}
	var effects []map[string]any
	if err := json.Unmarshal(storedRun.Effects, &effects); err != nil {
		t.Fatalf("decode effects: %v", err)
	}
	if len(effects) != 1 || effects[0]["message_id"] != "om_result" || effects[0]["purpose"] != "task_result" {
		t.Fatalf("delivery effects = %#v", effects)
	}
	var executionResult map[string]any
	if err := json.Unmarshal(storedTask.ExecutionResult, &executionResult); err != nil {
		t.Fatalf("decode Task execution_result: %v", err)
	}
	if delivered, ok := executionResult["effects"].([]any); !ok || len(delivered) != 1 {
		t.Fatalf("Task execution_result effects = %#v", executionResult["effects"])
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

// An empty user_message is the model's decision to stay silent in the source
// conversation, whatever the execution status is. No status — not observing, not
// waiting, not a crashed run that returned nothing — may become a placeholder
// reply such as "处理完成。", and none of them may overwrite an answer the model
// already delivered there.
func TestNotifyTaskFeedbackStaysSilentWithoutUserMessage(t *testing.T) {
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

	if err := executor.notifyTaskFeedback(t.Context(), &ExecuteResult{
		TaskID: 453, RunID: run.ID, Status: "observing", UserMessage: "",
	}); err != nil {
		t.Fatalf("notifyTaskFeedback() error = %v", err)
	}
	if err := executor.notifyTaskFeedback(t.Context(), &ExecuteResult{
		TaskID: 453, RunID: run.ID, Status: "done", UserMessage: "答案是 42",
	}); err != nil {
		t.Fatalf("notifyTaskFeedback() error = %v", err)
	}
	if err := executor.notifyTaskFeedback(t.Context(), &ExecuteResult{
		TaskID: 453, RunID: run.ID, Status: "waiting", UserMessage: "   ",
	}); err != nil {
		t.Fatalf("notifyTaskFeedback() error = %v", err)
	}
	// A run killed by a timeout or a restart returns no structured result at
	// all, so silence must fall out of the empty text rather than a status check.
	if err := executor.notifyTaskFeedback(t.Context(), &ExecuteResult{
		TaskID: 453, RunID: run.ID, Status: "failed",
	}); err != nil {
		t.Fatalf("notifyTaskFeedback() error = %v", err)
	}
	if feedback.replyCalls != 1 || feedback.updateCalls != 0 {
		t.Fatalf("feedback calls = reply:%d update:%d, want the single real answer only", feedback.replyCalls, feedback.updateCalls)
	}
}

// Once a result reply has been recalled it is gone from Feishu, so the next
// result must open a fresh reply instead of editing the vanished message.
func TestNotifyTaskFeedbackRepliesAgainAfterResultWasRecalled(t *testing.T) {
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
		Effects: datatypes.JSON(json.RawMessage(
			`[{"kind":"feishu_message","purpose":"task_result","message_id":"om_recalled","recalled_at":"2026-08-17T08:00:00Z"}]`)),
	}
	if err := store.SaveRun(t.Context(), run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	feedback := &successfulTaskFeedback{}
	executor := &AgentExecutor{store: store, feedback: feedback}

	if err := executor.notifyTaskFeedback(t.Context(), &ExecuteResult{
		TaskID: 453, RunID: run.ID, Status: "done", UserMessage: "答案是 42",
	}); err != nil {
		t.Fatalf("notifyTaskFeedback() error = %v", err)
	}
	if feedback.replyCalls != 1 || feedback.updateCalls != 0 {
		t.Fatalf("feedback calls = reply:%d update:%d, want a fresh reply", feedback.replyCalls, feedback.updateCalls)
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
