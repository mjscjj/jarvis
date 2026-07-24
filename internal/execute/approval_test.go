package execute

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"

	"gorm.io/datatypes"
)

// TestParseProposeResultHighRisk accepts a high-risk verdict that carries a full
// proposal (action + target + artifact).
func TestParseProposeResultHighRisk(t *testing.T) {
	msg := `{"needs_approval":true,"outcome":"needs_human","summary":"高风险：将更新飞书文档","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"更新周报文档","target":"周报 doc token=abc","artifact":"# 周报\n本周完成了 X。"},"waiting":null}`
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
		"nil proposal":   `{"needs_approval":true,"outcome":"needs_human","summary":"要审批","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null,"waiting":null}`,
		"empty artifact": `{"needs_approval":true,"outcome":"needs_human","summary":"要审批","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"发消息","target":"群 X","artifact":""},"waiting":null}`,
		"empty target":   `{"needs_approval":true,"outcome":"needs_human","summary":"要审批","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"发消息","target":"","artifact":"你好"},"waiting":null}`,
		"blank summary":  `{"needs_approval":true,"outcome":"needs_human","summary":"","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"a","target":"b","artifact":"c"},"waiting":null}`,
		"unknown field":  `{"needs_approval":true,"outcome":"needs_human","summary":"x","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"a","target":"b","artifact":"c"},"waiting":null,"extra":1}`,
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
	msg := `{"needs_approval":false,"outcome":"completed","summary":"已给自己发提醒","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null,"waiting":null}`
	result, err := parseProposeResult(msg)
	if err != nil {
		t.Fatalf("parseProposeResult() error = %v", err)
	}
	if result.NeedsApproval || result.Outcome != "completed" {
		t.Fatalf("result = %#v", result)
	}
}

// TestParseProposeResultLowRiskFailureNeedsReason keeps the existing fail-fast:
// a failed low-risk verdict must explain why.
func TestParseProposeResultLowRiskFailureNeedsReason(t *testing.T) {
	msg := `{"needs_approval":false,"outcome":"failed","summary":"没做成","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null,"waiting":null}`
	if _, err := parseProposeResult(msg); err == nil {
		t.Fatalf("outcome=failed without failure_reason must fail")
	}
}

func TestParseExecutionResultWaiting(t *testing.T) {
	msg := `{"outcome":"waiting","summary":"会议仍在进行","failure_reason":"","needs_followup":"","enrichments":[],"waiting":{"scheduled_task_id":42,"wake_at":"2026-07-23T16:30:00+08:00","reason":"稍后检查妙记"}}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if result.Outcome != "waiting" || result.Waiting == nil || result.Waiting.ScheduledTaskID != 42 {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseExecutionResultRejectsUnscheduledWaiting(t *testing.T) {
	msg := `{"outcome":"waiting","summary":"稍后再看","failure_reason":"","needs_followup":"","enrichments":[],"waiting":null}`
	if _, err := parseExecutionResult(msg); err == nil {
		t.Fatal("outcome=waiting without a scheduled task must fail")
	}
}

func TestParseExecutionResultNeedsHuman(t *testing.T) {
	msg := `{"outcome":"needs_human","summary":"授权页已打开","failure_reason":"","needs_followup":"请确认是否点击授权","enrichments":[],"waiting":null}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if result.Outcome != "needs_human" || result.NeedsFollowup != "请确认是否点击授权" {
		t.Fatalf("result = %#v", result)
	}

	blankFollowup := `{"outcome":"needs_human","summary":"需要人工","failure_reason":"","needs_followup":"","enrichments":[],"waiting":null}`
	if _, err := parseExecutionResult(blankFollowup); err == nil {
		t.Fatal("outcome=needs_human without needs_followup must fail")
	}
}

func TestParseExecutionResultAcceptsStringEnrichmentContent(t *testing.T) {
	msg := `{"outcome":"completed","summary":"已完成","failure_reason":"","needs_followup":"","enrichments":[{"kind":"code_link","label":"核心修改","content":"internal/execute/prompt.go"},{"kind":"risk","label":"风险","content":"需要同步前端"}],"waiting":null}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if len(result.Enrichments) != 2 {
		t.Fatalf("enrichments len = %d, want 2", len(result.Enrichments))
	}
	if got := result.Enrichments[0].Content; got != "internal/execute/prompt.go" {
		t.Fatalf("content = %q", got)
	}
}

// TestParseExecutionResultRejectsNonStringEnrichmentContent locks the contract:
// content is a string (codex's OpenAI Structured Outputs schema rejects a
// typeless open node). A JSON object/array for content must fail to decode.
func TestParseExecutionResultRejectsNonStringEnrichmentContent(t *testing.T) {
	for name, msg := range map[string]string{
		"object content": `{"outcome":"completed","summary":"已完成","failure_reason":"","needs_followup":"","enrichments":[{"kind":"code_link","label":"核心修改","content":{"path":"x"}}],"waiting":null}`,
		"array content":  `{"outcome":"completed","summary":"已完成","failure_reason":"","needs_followup":"","enrichments":[{"kind":"risk","label":"风险","content":["需要同步前端"]}],"waiting":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseExecutionResult(msg); err == nil {
				t.Fatalf("parseExecutionResult(%s) succeeded, want fail-fast", name)
			}
		})
	}
}

func TestParseExecutionResultRejectsLegacyEnrichmentDetail(t *testing.T) {
	msg := `{"outcome":"completed","summary":"已完成","failure_reason":"","needs_followup":"","enrichments":[{"kind":"code_link","label":"核心修改","detail":"internal/execute/prompt.go"}],"waiting":null}`
	if _, err := parseExecutionResult(msg); err == nil {
		t.Fatal("legacy detail enrichment must fail; content is the only accepted semantic payload")
	}
}

func TestParseExecutionResultDropsStrippedCodexMemoryCitation(t *testing.T) {
	// Codex memories instruct the model to append <oai-mem-citation> as the last
	// content of the final reply. With --output-schema the whole reply must be
	// JSON, so the model parks the block in enrichments[].content; Codex then
	// strips it from --output-last-message, leaving content="". Drop only that
	// known placeholder so the real task enrichments survive.
	msg := `{"outcome":"needs_human","summary":"已完成最小修复，推送需提权","failure_reason":"","needs_followup":"请提升仓库推送权限后恢复","enrichments":[{"kind":"evidence","label":"权限核验","content":"accessLevel=reporter"},{"kind":"memory_citation","label":"Memory sources","content":""}],"waiting":null}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if len(result.Enrichments) != 1 {
		t.Fatalf("enrichments len = %d, want 1 after dropping stripped memory_citation", len(result.Enrichments))
	}
	if got := result.Enrichments[0]; got.Kind != "evidence" || got.Content != "accessLevel=reporter" {
		t.Fatalf("remaining enrichment = %+v", got)
	}
}

func TestParseExecutionResultDropsBlankMemoryCitationRegardlessOfLabel(t *testing.T) {
	// Task #78 regression: Codex sometimes labels the stripped citation placeholder
	// "Memory citation" instead of "Memory sources". Any blank memory_citation is
	// the same Codex strip artifact and must be dropped; other blank enrichments
	// still fail-fast.
	msg := `{"outcome":"needs_human","summary":"已推送 MR","failure_reason":"","needs_followup":"请 Approve MR","enrichments":[{"kind":"merge_request","label":"MR !5","content":"https://example.com/mr/5"},{"kind":"memory_citation","label":"Memory citation","content":""}],"waiting":null}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if len(result.Enrichments) != 1 {
		t.Fatalf("enrichments len = %d, want 1 after dropping blank memory_citation", len(result.Enrichments))
	}
	if got := result.Enrichments[0]; got.Kind != "merge_request" || got.Content != "https://example.com/mr/5" {
		t.Fatalf("remaining enrichment = %+v", got)
	}
}

func TestParseExecutionResultStillRejectsBlankNonMemoryEnrichment(t *testing.T) {
	msg := `{"outcome":"completed","summary":"已完成","failure_reason":"","needs_followup":"","enrichments":[{"kind":"evidence","label":"证据","content":""}],"waiting":null}`
	if _, err := parseExecutionResult(msg); err == nil {
		t.Fatal("blank non-memory enrichment content must still fail-fast")
	}
}

func TestParseExecutionResultRejectsIncompleteEnrichment(t *testing.T) {
	cases := map[string]string{
		"blank kind":      `{"outcome":"completed","summary":"已完成","failure_reason":"","needs_followup":"","enrichments":[{"kind":"","label":"核心修改","content":"x"}],"waiting":null}`,
		"blank label":     `{"outcome":"completed","summary":"已完成","failure_reason":"","needs_followup":"","enrichments":[{"kind":"code_link","label":"","content":"x"}],"waiting":null}`,
		"missing content": `{"outcome":"completed","summary":"已完成","failure_reason":"","needs_followup":"","enrichments":[{"kind":"code_link","label":"核心修改"}],"waiting":null}`,
		"null content":    `{"outcome":"completed","summary":"已完成","failure_reason":"","needs_followup":"","enrichments":[{"kind":"code_link","label":"核心修改","content":null}],"waiting":null}`,
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseExecutionResult(msg); err == nil {
				t.Fatalf("parseExecutionResult(%s) succeeded, want fail-fast", name)
			}
		})
	}
}

func TestParseProposeResultDropsStrippedCodexMemoryCitation(t *testing.T) {
	msg := `{"needs_approval":true,"outcome":"needs_human","summary":"等待审批","failure_reason":"","needs_followup":"请检查产物","enrichments":[{"kind":"evidence","label":"写入依据","content":"doc_token=doc_x"},{"kind":"memory_citation","label":"Memory sources","content":""}],"proposal":{"action":"更新文档","target":"doc_x","artifact":"完整文档正文"},"waiting":null}`
	result, err := parseProposeResult(msg)
	if err != nil {
		t.Fatalf("parseProposeResult() error = %v", err)
	}
	if len(result.Enrichments) != 1 {
		t.Fatalf("enrichments len = %d, want 1", len(result.Enrichments))
	}
	if got := result.Enrichments[0].Content; got != "doc_token=doc_x" {
		t.Fatalf("content = %q", got)
	}
}

func TestParseProposeResultAcceptsStringEnrichmentContent(t *testing.T) {
	msg := `{"needs_approval":true,"outcome":"needs_human","summary":"等待审批","failure_reason":"","needs_followup":"请检查产物","enrichments":[{"kind":"evidence","label":"写入依据","content":"doc_token=doc_x; 章节: 进展/风险"}],"proposal":{"action":"更新文档","target":"doc_x","artifact":"完整文档正文"},"waiting":null}`
	result, err := parseProposeResult(msg)
	if err != nil {
		t.Fatalf("parseProposeResult() error = %v", err)
	}
	if len(result.Enrichments) != 1 {
		t.Fatalf("enrichments len = %d, want 1", len(result.Enrichments))
	}
	if got := result.Enrichments[0].Content; got != "doc_token=doc_x; 章节: 进展/风险" {
		t.Fatalf("content = %q", got)
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
	good := []byte(`{"needs_approval":true,"outcome":"needs_human","summary":"要审批","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"发周报","target":"研发群 chat_id=xyz","artifact":"本周进展：AAA"},"waiting":null}`)
	got := proposalFromRunOutput(good)
	if got == nil || got.Action != "发周报" || got.Target != "研发群 chat_id=xyz" || got.Artifact != "本周进展：AAA" {
		t.Fatalf("proposalFromRunOutput(good) = %#v, want full proposal", got)
	}
	for name, raw := range map[string][]byte{
		"low risk (no approval)": []byte(`{"needs_approval":false,"outcome":"completed","summary":"已做完","proposal":null}`),
		"nil proposal":           []byte(`{"needs_approval":true,"proposal":null}`),
		"empty artifact":         []byte(`{"needs_approval":true,"proposal":{"action":"a","target":"b","artifact":""}}`),
		"empty target":           []byte(`{"needs_approval":true,"proposal":{"action":"a","target":"","artifact":"c"}}`),
		"final run result":       []byte(`{"outcome":"completed","summary":"done"}`),
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
	run := &domain.ExecutionRun{ID: 135, ActionType: "summary_post", Sandbox: "danger-full-access", Status: "failed"}
	ok := runResultPayload(run, nil)
	if ok["stage"] != "executed" {
		t.Fatalf("success payload stage = %v, want executed", ok["stage"])
	}
	if ok["source_run_id"] != uint64(135) {
		t.Fatalf("success payload source_run_id = %v, want 135", ok["source_run_id"])
	}
	if _, hasErr := ok["error"]; hasErr {
		t.Fatalf("success payload must not carry error: %#v", ok)
	}
	failed := runResultPayload(run, errTest)
	if failed["stage"] != "executed" || failed["error"] != errTest.Error() {
		t.Fatalf("failed payload = %#v, want stage=executed + error", failed)
	}
	interrupted := runResultPayload(run, fmt.Errorf("stop requested: %w", ErrExecutionInterrupted))
	if interrupted["stage"] != "interrupted" || interrupted["error"] == nil {
		t.Fatalf("interrupted payload = %#v, want stage=interrupted + error", interrupted)
	}
	encoded, err := json.Marshal(interrupted)
	if err != nil || !resultHasStage(encoded, "interrupted") {
		t.Fatalf("resultHasStage(interrupted) = false, payload=%s err=%v", encoded, err)
	}
}

var errTest = errors.New("group not found")

func TestBuildHumanResumePrompt(t *testing.T) {
	prompt, err := buildHumanResumePrompt("test M5 system prompt", "我已确认授权，请继续", "", testToolCatalog)
	if err != nil {
		t.Fatalf("buildHumanResumePrompt() error = %v", err)
	}
	for _, want := range []string{
		"我已确认授权，请继续",
		"phase=resume_human",
		"同一个 Task、同一个 Session",
		"不重跑、不重复副作用",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("human resume prompt missing %q:\n%s", want, prompt)
		}
	}
}

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

// TestBuildProposePrompt verifies the propose prompt injects the editable policy.
func TestBuildProposePrompt(t *testing.T) {
	task := &domain.Task{
		ID: 11, Title: "更新周报", ActionType: "doc_write",
		Plan: datatypes.JSON(`{"steps":["update"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	prompt, err := buildProposePrompt("test M5 system prompt", "修改文件需要审批。", task, testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("buildProposePrompt() error = %v", err)
	}
	for _, want := range []string{"phase=propose", "BEGIN_APPROVAL_POLICY", "修改文件需要审批。", "proposal", "BEGIN_TASK_CONTEXT"} {
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
	empty, err := buildProposePrompt("test M5 system prompt", "只读不审批。", task, testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("buildProposePrompt() error = %v", err)
	}
	if strings.Contains(empty, "BEGIN_SHARED_MEMORY") {
		t.Fatalf("empty shared memory must not inject block:\n%s", empty)
	}
	prompt, err := buildProposePrompt("test M5 system prompt", "只读不审批。", task, testToolCatalog, "周报模板固定用飞书文档 xxx", "", "", nil)
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
	prompt, err := buildApplyPrompt("test M5 system prompt", task, proposal, testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("buildApplyPrompt() error = %v", err)
	}
	for _, want := range []string{"phase=apply", "已获批准", "不重新拟稿", "不得重复执行", "本周关键进展如下：AAA", "APPROVED_PROPOSAL", "研发群 chat_id=xyz"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("apply prompt missing %q", want)
		}
	}
}

func TestBuildProposePromptRequiresApprovalPolicy(t *testing.T) {
	task := &domain.Task{ID: 14, Title: "x", ActionType: "doc_write", Plan: datatypes.JSON(`{}`), Background: datatypes.JSON(`{}`)}
	if _, err := buildProposePrompt("test M5 system prompt", "", task, testToolCatalog, "", "", "", nil); err == nil {
		t.Fatal("empty approval policy must fail")
	}
}

// TestBuildApplyPromptRequiresProposal fails-fast when no proposal is given.
func TestBuildApplyPromptRequiresProposal(t *testing.T) {
	task := &domain.Task{ID: 13, Title: "x", ActionType: "doc_write", Plan: datatypes.JSON(`{}`), Background: datatypes.JSON(`{}`)}
	if _, err := buildApplyPrompt("test M5 system prompt", task, nil, testToolCatalog, "", "", "", nil); err == nil {
		t.Fatalf("nil proposal must fail")
	}
}

// TestInvestigateGoesThroughPropose verifies the gate rule for a NON-code_change,
// non-traditionally-external action (investigate): it does not run to completion,
// it goes through the propose stage, and its propose prompt still asks the agent
// to judge — by intent — whether it will touch the outside world. This closes the
// "an investigate Task decides mid-run to send a message" gap.
func TestInvestigateGoesThroughPropose(t *testing.T) {
	if runsToCompletion(&domain.Task{ActionType: "investigate", ExecutionMode: taskcreate.ExecutionModeStandard}) {
		t.Fatalf("investigate must go through propose, not run to completion")
	}
	task := &domain.Task{
		ID: 21, Title: "查证登录超时", ActionType: "investigate",
		Plan: datatypes.JSON(`{"steps":["read logs"]}`), Background: datatypes.JSON(`{"snapshot_version":"v1"}`),
	}
	prompt, err := buildProposePrompt("test M5 system prompt", "所有写操作需要审批。", task, testToolCatalog, "", "", "", nil)
	if err != nil {
		t.Fatalf("buildProposePrompt(investigate) error = %v", err)
	}
	for _, want := range []string{"phase=propose", "APPROVAL_POLICY", "所有写操作需要审批。", "proposal"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("investigate propose prompt missing %q", want)
		}
	}
}

func TestDirectTaskStillGoesThroughApproval(t *testing.T) {
	task := &domain.Task{ActionType: "agent_task", ExecutionMode: taskcreate.ExecutionModeDirect}
	if runsToCompletion(task) {
		t.Fatal("direct agent_task must not skip propose/approval")
	}
}

func TestCodeChangeStillUsesMRReviewGate(t *testing.T) {
	task := &domain.Task{ActionType: "code_change", ExecutionMode: taskcreate.ExecutionModeStandard}
	if !runsToCompletion(task) {
		t.Fatal("code_change must keep its direct execution + MR review path")
	}
}

func TestValidateTaskIntegrityRejectsDrift(t *testing.T) {
	plan := json.RawMessage(`{"instruction":"加入会议"}`)
	hash, err := taskcreate.ActionHash("agent_task", "会议", plan)
	if err != nil {
		t.Fatalf("ActionHash() error = %v", err)
	}
	task := &domain.Task{
		ID: 1, ActionType: "agent_task", Target: "会议", Plan: datatypes.JSON(plan),
		ActionHash: hash, ExecutionMode: taskcreate.ExecutionModeDirect,
	}
	if err := validateTaskIntegrity(task); err != nil {
		t.Fatalf("validateTaskIntegrity() error = %v", err)
	}
	task.Target = "被篡改的会议"
	if err := validateTaskIntegrity(task); err == nil {
		t.Fatal("validateTaskIntegrity() accepted action drift")
	}
}

// TestProposeRoutingReadOnlyVsMutation documents the two propose outcomes: a
// read-only investigation finishes in place, while any intended mutation parks
// with a complete proposal.
func TestProposeRoutingReadOnlyVsMutation(t *testing.T) {
	readOnly := `{"needs_approval":false,"outcome":"completed","summary":"已读日志得出结论","failure_reason":"","needs_followup":"","enrichments":[],"proposal":null,"waiting":null}`
	low, err := parseProposeResult(readOnly)
	if err != nil {
		t.Fatalf("read-only parse error = %v", err)
	}
	if low.NeedsApproval || low.Outcome != "completed" {
		t.Fatalf("read-only investigate should finish in place: %#v", low)
	}

	mutation := `{"needs_approval":true,"outcome":"needs_human","summary":"查证中需要发消息给对方","failure_reason":"","needs_followup":"","enrichments":[],"proposal":{"action":"向对方发确认消息","target":"张三 open_id=ou_x","artifact":"你好，关于登录超时想确认一下……"},"waiting":null}`
	high, err := parseProposeResult(mutation)
	if err != nil {
		t.Fatalf("mutation parse error = %v", err)
	}
	if !high.NeedsApproval || high.Proposal == nil || high.Proposal.Artifact == "" {
		t.Fatalf("high-risk investigate should park with a full proposal: %#v", high)
	}
}
