package execute

import (
	"encoding/json"
	"testing"

	"jarvis/internal/contextsnap"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
)

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
