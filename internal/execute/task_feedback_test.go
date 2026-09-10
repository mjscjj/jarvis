package execute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/contextpack"
	"jarvis/internal/contextsnap"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type successfulTaskFeedback struct {
	reactionCalls       int
	removeReactionCalls int
	removed             TaskFeedbackReaction
}

func (f *successfulTaskFeedback) AddProcessingReaction(_ context.Context, target TaskFeedbackTarget) (*TaskFeedbackReaction, error) {
	f.reactionCalls++
	return &TaskFeedbackReaction{SourceMessageID: target.SourceMessageID, ReactionID: "reaction_on_it"}, nil
}

func (f *successfulTaskFeedback) RemoveProcessingReaction(_ context.Context, reaction TaskFeedbackReaction) error {
	f.removeReactionCalls++
	f.removed = reaction
	return nil
}

type unavailableTaskFeedback struct {
	reactionCalls       int
	removeReactionCalls int
}

func (f *unavailableTaskFeedback) AddProcessingReaction(context.Context, TaskFeedbackTarget) (*TaskFeedbackReaction, error) {
	f.reactionCalls++
	return nil, errors.New("230002 Bot/User can NOT be out of the chat")
}

func (f *unavailableTaskFeedback) RemoveProcessingReaction(context.Context, TaskFeedbackReaction) error {
	f.removeReactionCalls++
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
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY, todo_id INTEGER, title TEXT NOT NULL DEFAULT '',
			action_type TEXT NOT NULL DEFAULT '', target TEXT NOT NULL DEFAULT '',
			source_payload JSON NOT NULL DEFAULT '{}',
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

func TestTaskFeedbackTargetUsesNewestFeishuEvidence(t *testing.T) {
	raw, err := (contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		Principal:       &contextsnap.Principal{OpenID: "ou_me", Name: "我"},
		Messages: []contextsnap.Message{
			{MessageID: "meeting:1", CreateTime: 30},
			{MessageID: "om_old", CreateTime: 10},
			{MessageID: "om_new", ThreadID: "omt_topic", ChatMode: "group", CreateTime: 20},
		},
	}).Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := taskFeedbackTarget(frozenTestContent(`{"source_message_ids":["om_old","om_new","meeting:1"]}`, string(raw)))
	if err != nil || got.SourceMessageID != "om_new" {
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

	if err := executor.startTaskFeedback(t.Context(), &domain.Task{ID: 453, SourcePayload: frozenTestContent(`{"source_message_ids":["om_human_p2p"]}`, string(raw))}, run); err != nil {
		t.Fatalf("startTaskFeedback() error = %v, want unavailable source ignored", err)
	}
	if feedback.reactionCalls != 1 {
		t.Fatalf("reaction calls = %d, want 1", feedback.reactionCalls)
	}
	if run.Status != "running" || len(run.Effects) != 0 {
		t.Fatalf("run changed by unavailable reaction: status=%s effects=%s", run.Status, run.Effects)
	}
}

func TestStartTaskFeedbackRecordsOnItEffect(t *testing.T) {
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
	run := &domain.ExecutionRun{
		TaskID: 453, ActionType: "investigate", Stage: "execute", Sandbox: "danger-full-access",
		Status: "running", Prompt: "test", StartedAt: time.Now().UTC(),
	}
	if err := store.SaveRun(t.Context(), run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	feedback := &successfulTaskFeedback{}
	executor := &AgentExecutor{store: store, feedback: feedback}

	if err := executor.startTaskFeedback(t.Context(), &domain.Task{ID: 453, SourcePayload: frozenTestContent(`{"source_message_ids":["om_source"]}`, string(raw))}, run); err != nil {
		t.Fatalf("startTaskFeedback() error = %v", err)
	}
	if feedback.reactionCalls != 1 {
		t.Fatalf("reaction calls = %d, want 1", feedback.reactionCalls)
	}
	stored, err := store.LoadRun(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	var effects []map[string]any
	if err := json.Unmarshal(stored.Effects, &effects); err != nil {
		t.Fatalf("decode effects: %v", err)
	}
	if len(effects) != 1 || effects[0]["purpose"] != "task_processing" || effects[0]["operation"] != "add" {
		t.Fatalf("effects = %#v", effects)
	}
}

func TestPersistTerminalRunRemovesOnItReaction(t *testing.T) {
	store := newTaskFeedbackTestStore(t)
	run := &domain.ExecutionRun{
		TaskID: 453, ActionType: "investigate", Stage: "execute", Sandbox: "danger-full-access",
		Status: "succeeded", Prompt: "test", StartedAt: time.Now().UTC(),
		Effects: datatypes.JSON(`[
			{"kind":"feishu_reaction","purpose":"task_processing","source_message_id":"om_source","reaction_id":"reaction_on_it","operation":"add"},
			{"kind":"file","title":"产物"}
		]`),
	}
	feedback := &successfulTaskFeedback{}
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	executor := &AgentExecutor{store: store, feedback: feedback, now: func() time.Time { return now }}

	if err := executor.persistRun(t.Context(), run); err != nil {
		t.Fatalf("persist initial run: %v", err)
	}
	executor.finishTaskFeedback(t.Context(), run)
	if feedback.removeReactionCalls != 1 ||
		feedback.removed.SourceMessageID != "om_source" ||
		feedback.removed.ReactionID != "reaction_on_it" {
		t.Fatalf("removed reaction = %#v calls=%d", feedback.removed, feedback.removeReactionCalls)
	}
	stored, err := store.LoadRun(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	var effects []map[string]any
	if err := json.Unmarshal(stored.Effects, &effects); err != nil {
		t.Fatalf("decode effects: %v", err)
	}
	if effects[0]["removed_at"] != now.Format(time.RFC3339) {
		t.Fatalf("removed effect = %#v", effects[0])
	}
	if _, exists := effects[1]["removed_at"]; exists {
		t.Fatalf("unrelated effect marked removed: %#v", effects[1])
	}
}

func TestPersistTerminalRunKeepsUnremovedOnItWhenCleanupFails(t *testing.T) {
	store := newTaskFeedbackTestStore(t)
	run := &domain.ExecutionRun{
		TaskID: 453, ActionType: "investigate", Stage: "execute", Sandbox: "danger-full-access",
		Status: "failed", Prompt: "test", StartedAt: time.Now().UTC(),
		Effects: datatypes.JSON(`[{
			"kind":"feishu_reaction","purpose":"task_processing",
			"source_message_id":"om_source","reaction_id":"reaction_on_it"
		}]`),
	}
	feedback := &unavailableTaskFeedback{}
	executor := &AgentExecutor{store: store, feedback: feedback, now: time.Now}

	if err := executor.persistRun(t.Context(), run); err != nil {
		t.Fatalf("persist initial run: %v", err)
	}
	executor.finishTaskFeedback(t.Context(), run)
	if feedback.removeReactionCalls != 1 {
		t.Fatalf("remove calls = %d, want 1", feedback.removeReactionCalls)
	}
	if strings.Contains(string(run.Effects), "removed_at") {
		t.Fatalf("failed cleanup was marked successful: %s", run.Effects)
	}
}

func TestRouteRunTreatsLegacyOutputFieldsAsAuditOnly(t *testing.T) {
	store := newTaskFeedbackTestStore(t)
	if err := store.db.Exec(
		`INSERT INTO task(id, title, action_type, source_payload, status, version)
		 VALUES (?, ?, ?, '{}', 'executing', 0)`,
		453, "完成任务", "investigate",
	).Error; err != nil {
		t.Fatalf("insert Task: %v", err)
	}
	task, err := store.LoadTask(t.Context(), 453)
	if err != nil {
		t.Fatalf("LoadTask() error = %v", err)
	}
	summary := "任务已经完成"
	startedAt := time.Now().UTC().Add(-time.Second)
	finishedAt := time.Now().UTC()
	durationMs := finishedAt.Sub(startedAt).Milliseconds()
	run := &domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Stage: "execute",
		Sandbox: "danger-full-access", Status: "succeeded", Prompt: "test",
		Summary: &summary,
		Output: datatypes.JSON(json.RawMessage(
			`{"outcome":"completed","summary":"任务已经完成","user_message":"旧协议内容","progress_summary":""}`)),
		StartedAt: startedAt, FinishedAt: &finishedAt, DurationMs: &durationMs,
	}
	executor := &AgentExecutor{store: store, now: time.Now}
	result, err := executor.routeRun(t.Context(), task, task.Version, run, &codexResult{Outcome: "completed", Summary: summary}, nil)
	if err != nil {
		t.Fatalf("routeRun() error = %v", err)
	}
	if result == nil || result.Status != "done" {
		t.Fatalf("routeRun() result = %#v, want done", result)
	}
	stored, err := store.LoadTask(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("LoadTask() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stored.ExecutionResult, &payload); err != nil {
		t.Fatalf("decode execution_result: %v", err)
	}
	if _, exists := payload["user_message"]; exists {
		t.Fatalf("legacy output field was projected into current execution_result: %#v", payload)
	}
}

func TestRecordAgentVerdictPreservesRuntimeFeedbackEffect(t *testing.T) {
	run := &domain.ExecutionRun{Effects: datatypes.JSON(json.RawMessage(`[{
		"kind":"feishu_reaction","purpose":"task_processing","reaction_id":"reaction_on_it"
	}]`))}
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

func TestTaskFeedbackIgnoresAnnotationAndUncitedConversation(t *testing.T) {
	raw, err := contextpack.Freeze(
		[]byte(`{"source_message_ids":["om_source"]}`),
		[]byte(`{"messages":[{"message_id":"om_source","create_time":10},{"message_id":"om_unrelated","create_time":100}]}`),
		"brief", []byte(`{"source_message_ids":["om_unrelated"],"messages":[{"message_id":"om_invented","create_time":999}]}`))
	if err != nil {
		t.Fatal(err)
	}
	target, err := taskFeedbackTarget(raw)
	if err != nil || target.SourceMessageID != "om_source" {
		t.Fatalf("feedback used unvalidated target: %#v %v", target, err)
	}
}
