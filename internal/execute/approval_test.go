package execute

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
)

// TestParseProposeResultHighRisk accepts a high-risk verdict that carries a full
// proposal (action + target + artifact).
func TestParseProposeResultHighRisk(t *testing.T) {
	msg := `{"needs_approval":true,"success":false,"summary":"高风险：将更新飞书文档","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"更新周报文档","target":"周报 doc token=abc","artifact":"# 周报\n本周完成了 X。"}}`
	result, err := parseProposeResult(msg)
	if err != nil {
		t.Fatalf("parseProposeResult() error = %v", err)
	}
	if !result.NeedsApproval || result.Proposal == nil || result.Proposal.Artifact == "" {
		t.Fatalf("result = %#v", result)
	}
}

// TestParseProposeResultRejectsMissingProposal is the core fail-fast: a
// needs_approval=true verdict with no proposal (or an empty artifact) is useless
// and must be an execution failure, not a silent stop.
func TestParseProposeResultRejectsMissingProposal(t *testing.T) {
	cases := map[string]string{
		"nil proposal":   `{"needs_approval":true,"success":false,"summary":"要审批","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null}`,
		"empty artifact": `{"needs_approval":true,"success":false,"summary":"要审批","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"发消息","target":"群 X","artifact":""}}`,
		"empty target":   `{"needs_approval":true,"success":false,"summary":"要审批","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"发消息","target":"","artifact":"你好"}}`,
		"blank summary":  `{"needs_approval":true,"success":false,"summary":"","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"a","target":"b","artifact":"c"}}`,
		"unknown field":  `{"needs_approval":true,"success":false,"summary":"x","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"a","target":"b","artifact":"c"},"extra":1}`,
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseProposeResult(msg); err == nil {
				t.Fatalf("parseProposeResult(%s) succeeded, want fail-fast", name)
			}
		})
	}
}

// TestParseProposeResultLowRisk accepts a low-risk verdict where the agent
// already finished the work (needs_approval=false, no proposal required).
func TestParseProposeResultLowRisk(t *testing.T) {
	msg := `{"needs_approval":false,"success":true,"summary":"已给自己发提醒","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null}`
	result, err := parseProposeResult(msg)
	if err != nil {
		t.Fatalf("parseProposeResult() error = %v", err)
	}
	if result.NeedsApproval || !result.Success {
		t.Fatalf("result = %#v", result)
	}
}

// TestParseProposeResultLowRiskFailureNeedsReason keeps the existing fail-fast:
// a failed low-risk verdict must explain why.
func TestParseProposeResultLowRiskFailureNeedsReason(t *testing.T) {
	msg := `{"needs_approval":false,"success":false,"summary":"没做成","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null}`
	if _, err := parseProposeResult(msg); err == nil {
		t.Fatalf("success=false without failure_reason must fail")
	}
}

// TestProposalPayloadRoundTrip checks the awaiting_approval execution_result we
// store can be decoded back into the artifact the apply stage needs.
func TestProposalPayloadRoundTrip(t *testing.T) {
	session := "thread-123"
	run := &domain.ExecutionRun{ActionType: "doc_write", CodexSessionID: &session}
	propose := &proposeResult{
		NeedsApproval: true,
		Summary:       "将更新文档",
		Proposal:      &codexProposal{Action: "更新文档", Target: "doc abc", Artifact: "全文内容"},
	}
	encoded, err := json.Marshal(proposalPayload(run, propose))
	if err != nil {
		t.Fatalf("marshal proposal payload: %v", err)
	}
	got, err := decodeStoredProposal(encoded)
	if err != nil {
		t.Fatalf("decodeStoredProposal() error = %v", err)
	}
	if got.Action != "更新文档" || got.Target != "doc abc" || got.Artifact != "全文内容" {
		t.Fatalf("decoded proposal = %#v", got)
	}
}

// TestDecodeStoredProposalRejectsNonProposal fails-fast when execution_result is
// a final run result (or a rejection), not a pending proposal.
func TestDecodeStoredProposalRejectsNonProposal(t *testing.T) {
	for name, raw := range map[string][]byte{
		"final result": []byte(`{"run_status":"succeeded","summary":"done"}`),
		"rejection":    []byte(`{"stage":"rejected","summary":"驳回"}`),
		"empty":        nil,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeStoredProposal(raw); err == nil {
				t.Fatalf("decodeStoredProposal(%s) succeeded, want fail-fast", name)
			}
		})
	}
}

// TestProposalFromRunOutput recovers the approved proposal from a propose run's
// output (needs_approval=true + full proposal), and returns nil for anything not
// approvable — the basis for "用同一已批准方案重试落地" (reapply).
func TestProposalFromRunOutput(t *testing.T) {
	good := []byte(`{"needs_approval":true,"success":false,"summary":"要审批","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"发周报","target":"研发群 chat_id=xyz","artifact":"本周进展：AAA"}}`)
	got := proposalFromRunOutput(good)
	if got == nil || got.Action != "发周报" || got.Target != "研发群 chat_id=xyz" || got.Artifact != "本周进展：AAA" {
		t.Fatalf("proposalFromRunOutput(good) = %#v, want full proposal", got)
	}
	for name, raw := range map[string][]byte{
		"low risk (no approval)": []byte(`{"needs_approval":false,"success":true,"summary":"已做完","proposal":null}`),
		"nil proposal":           []byte(`{"needs_approval":true,"proposal":null}`),
		"empty artifact":         []byte(`{"needs_approval":true,"proposal":{"action":"a","target":"b","artifact":""}}`),
		"empty target":           []byte(`{"needs_approval":true,"proposal":{"action":"a","target":"","artifact":"c"}}`),
		"final run result":       []byte(`{"success":true,"summary":"done"}`),
		"empty":                  nil,
		"garbage":                []byte(`not json`),
	} {
		t.Run(name, func(t *testing.T) {
			if got := proposalFromRunOutput(raw); got != nil {
				t.Fatalf("proposalFromRunOutput(%s) = %#v, want nil", name, got)
			}
		})
	}
}

// TestRunResultPayloadTagsStage verifies a codex-driven terminal result carries
// stage=executed (so the UI tells a real execution failure apart from a human
// rejection / manual mark-failed), and that a failure also carries the error.
func TestRunResultPayloadTagsStage(t *testing.T) {
	run := &domain.ExecutionRun{ActionType: "summary_post", Sandbox: "danger-full-access", Status: "failed"}
	ok := runResultPayload(run, nil)
	if ok["stage"] != "executed" {
		t.Fatalf("success payload stage = %v, want executed", ok["stage"])
	}
	if _, hasErr := ok["error"]; hasErr {
		t.Fatalf("success payload must not carry error: %#v", ok)
	}
	failed := runResultPayload(run, errTest)
	if failed["stage"] != "executed" || failed["error"] != errTest.Error() {
		t.Fatalf("failed payload = %#v, want stage=executed + error", failed)
	}
}

var errTest = errors.New("group not found")

// TestRejectionPayload keeps the rejection distinguishable from a codex failure.
func TestRejectionPayload(t *testing.T) {
	withReason := rejectionPayload("措辞不合适")
	if withReason["stage"] != "rejected" || withReason["reject_reason"] != "措辞不合适" {
		t.Fatalf("payload = %#v", withReason)
	}
	noReason := rejectionPayload("")
	if _, ok := noReason["reject_reason"]; ok {
		t.Fatalf("blank reason must be omitted: %#v", noReason)
	}
}

// TestBuildProposePromptExternal verifies the propose prompt tells the agent to
// judge risk and NOT touch the outside world for high-risk writes.
func TestBuildProposePrompt(t *testing.T) {
	task := &domain.Task{
		ID: 11, Title: "更新周报", ActionType: "doc_write",
		Plan: datatypes.JSON(`{"steps":["update"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	prompt, err := buildProposePrompt(task, "", "", nil)
	if err != nil {
		t.Fatalf("buildProposePrompt() error = %v", err)
	}
	for _, want := range []string{"方案阶段", "绝对不要真正写入", "needs_approval", "proposal", "BEGIN_TASK_CONTEXT"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("propose prompt missing %q", want)
		}
	}
}

// 共享记忆非空时，propose prompt 应在 TASK_CONTEXT 之前包含 BEGIN_SHARED_MEMORY 标记
// 与内容；为空时不包含。
func TestBuildProposePromptInjectsSharedMemory(t *testing.T) {
	task := &domain.Task{
		ID: 11, Title: "更新周报", ActionType: "doc_write",
		Plan: datatypes.JSON(`{"steps":["update"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	empty, err := buildProposePrompt(task, "", "", nil)
	if err != nil {
		t.Fatalf("buildProposePrompt() error = %v", err)
	}
	if strings.Contains(empty, "BEGIN_SHARED_MEMORY") {
		t.Fatalf("empty shared memory must not inject block:\n%s", empty)
	}
	prompt, err := buildProposePrompt(task, "周报模板固定用飞书文档 xxx", "", nil)
	if err != nil {
		t.Fatalf("buildProposePrompt() error = %v", err)
	}
	for _, want := range []string{"BEGIN_SHARED_MEMORY", "周报模板固定用飞书文档 xxx", "可信"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("propose prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Index(prompt, "BEGIN_SHARED_MEMORY") >= strings.Index(prompt, "BEGIN_TASK_CONTEXT") {
		t.Fatalf("shared memory block must precede TASK_CONTEXT:\n%s", prompt)
	}
}

// TestBuildApplyPromptEmbedsArtifact verifies the apply prompt embeds the approved
// artifact verbatim and instructs faithful landing.
func TestBuildApplyPromptEmbedsArtifact(t *testing.T) {
	task := &domain.Task{
		ID: 12, Title: "发周报", ActionType: "summary_post",
		Plan: datatypes.JSON(`{"steps":["send"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	proposal := &codexProposal{Action: "向群发送周报", Target: "研发群 chat_id=xyz", Artifact: "本周关键进展如下：AAA"}
	prompt, err := buildApplyPrompt(task, proposal, "", "", nil)
	if err != nil {
		t.Fatalf("buildApplyPrompt() error = %v", err)
	}
	for _, want := range []string{"落地阶段", "已获委托人批准", "本周关键进展如下：AAA", "APPROVED_PROPOSAL", "研发群 chat_id=xyz"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("apply prompt missing %q", want)
		}
	}
}

// TestBuildApplyPromptRequiresProposal fails-fast when no proposal is given.
func TestBuildApplyPromptRequiresProposal(t *testing.T) {
	task := &domain.Task{ID: 13, Title: "x", ActionType: "doc_write", Plan: datatypes.JSON(`{}`), Background: datatypes.JSON(`{}`)}
	if _, err := buildApplyPrompt(task, nil, "", "", nil); err == nil {
		t.Fatalf("nil proposal must fail")
	}
}

// TestInvestigateGoesThroughPropose verifies the gate rule for a NON-code_change,
// non-traditionally-external action (investigate): it does not run to completion,
// it goes through the propose stage, and its propose prompt still asks the agent
// to judge — by intent — whether it will touch the outside world. This closes the
// "an investigate Task decides mid-run to send a message" gap.
func TestInvestigateGoesThroughPropose(t *testing.T) {
	if runsToCompletion("investigate") {
		t.Fatalf("investigate must go through propose, not run to completion")
	}
	task := &domain.Task{
		ID: 21, Title: "查证登录超时", ActionType: "investigate",
		Plan: datatypes.JSON(`{"steps":["read logs"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	prompt, err := buildProposePrompt(task, "", "", nil)
	if err != nil {
		t.Fatalf("buildProposePrompt(investigate) error = %v", err)
	}
	for _, want := range []string{"方案阶段", "会不会真正碰到外部世界", "needs_approval", "只读/查询/产出本地结论"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("investigate propose prompt missing %q", want)
		}
	}
}

// TestProposeRoutingLowRiskVsHighRisk documents the two propose outcomes that the
// executor routes on: a read-only investigate finishes in place (needs_approval
// =false, success=true), while any intended external write parks for approval
// (needs_approval=true with a full proposal).
func TestProposeRoutingLowRiskVsHighRisk(t *testing.T) {
	lowRisk := `{"needs_approval":false,"success":true,"summary":"已读日志得出结论","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null}`
	low, err := parseProposeResult(lowRisk)
	if err != nil {
		t.Fatalf("low-risk parse error = %v", err)
	}
	if low.NeedsApproval || !low.Success {
		t.Fatalf("low-risk investigate should finish in place: %#v", low)
	}

	highRisk := `{"needs_approval":true,"success":false,"summary":"查证中需要发消息给对方","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"向对方发确认消息","target":"张三 open_id=ou_x","artifact":"你好，关于登录超时想确认一下……"}}`
	high, err := parseProposeResult(highRisk)
	if err != nil {
		t.Fatalf("high-risk parse error = %v", err)
	}
	if !high.NeedsApproval || high.Proposal == nil || high.Proposal.Artifact == "" {
		t.Fatalf("high-risk investigate should park with a full proposal: %#v", high)
	}
}
