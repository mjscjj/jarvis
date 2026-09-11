package execute

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"jarvis/internal/domain"

	"jarvis/internal/datatypes"
)

// TestParseQuestionResult accepts a verdict that stops to ask the principal for
// permission, carrying the full text it wants to send plus a button to answer.
func TestParseQuestionResult(t *testing.T) {
	msg := `{"outcome":"needs_human","progress_summary":"","summary":"要更新飞书文档，先请示","failure_reason":"","enrichments":[],"question":{"title":"要更新周报文档吗","body":"# 周报\n本周完成了 X。","fields":[{"type":"button","name":"go","label":"就这样写","options":[],"url":"","style":"primary"},{"type":"input","name":"note","label":"补充","options":[],"url":"","style":""}]},"effects":[],"waiting":null}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	question, err := ParseQuestion(result.Question)
	if err != nil {
		t.Fatalf("ParseQuestion() error = %v", err)
	}
	if question.Title != "要更新周报文档吗" || len(question.Fields) != 2 {
		t.Fatalf("question = %#v", question)
	}
}

// TestParseQuestionResultRejectsUnanswerableQuestion is the core fail-fast: a
// Task that stops for a human with nothing the human can answer would park
// forever, so it is an execution failure rather than a silent stop.
func TestParseQuestionResultRejectsUnanswerableQuestion(t *testing.T) {
	cases := map[string]string{
		"no question":    `{"outcome":"needs_human","progress_summary":"","summary":"要问人","failure_reason":"","enrichments":[],"question":null,"effects":[],"waiting":null}`,
		"blank title":    `{"outcome":"needs_human","progress_summary":"","summary":"要问人","failure_reason":"","enrichments":[],"question":{"title":" ","body":"x","fields":[{"type":"button","name":"go","label":"好","options":[],"url":"","style":""}]},"effects":[],"waiting":null}`,
		"no button":      `{"outcome":"needs_human","progress_summary":"","summary":"要问人","failure_reason":"","enrichments":[],"question":{"title":"选一个","body":"","fields":[{"type":"select","name":"plan","label":"方案","options":["A","B"],"url":"","style":""}]},"effects":[],"waiting":null}`,
		"empty options":  `{"outcome":"needs_human","progress_summary":"","summary":"要问人","failure_reason":"","enrichments":[],"question":{"title":"选一个","body":"","fields":[{"type":"button","name":"go","label":"好","options":[],"url":"","style":""},{"type":"select","name":"plan","label":"方案","options":[],"url":"","style":""}]},"effects":[],"waiting":null}`,
		"duplicate name": `{"outcome":"needs_human","progress_summary":"","summary":"要问人","failure_reason":"","enrichments":[],"question":{"title":"选一个","body":"","fields":[{"type":"button","name":"go","label":"好","options":[],"url":"","style":""},{"type":"input","name":"go","label":"补充","options":[],"url":"","style":""}]},"effects":[],"waiting":null}`,
		"unknown type":   `{"outcome":"needs_human","progress_summary":"","summary":"要问人","failure_reason":"","enrichments":[],"question":{"title":"选一个","body":"","fields":[{"type":"slider","name":"go","label":"好","options":[],"url":"","style":""}]},"effects":[],"waiting":null}`,
		"link no url":    `{"outcome":"needs_human","progress_summary":"","summary":"要问人","failure_reason":"","enrichments":[],"question":{"title":"看一下","body":"","fields":[{"type":"button","name":"go","label":"好","options":[],"url":"","style":""},{"type":"link","name":"","label":"详情","options":[],"url":"","style":""}]},"effects":[],"waiting":null}`,
		"blank summary":  `{"outcome":"needs_human","progress_summary":"","summary":"","failure_reason":"","enrichments":[],"question":{"title":"选一个","body":"","fields":[{"type":"button","name":"go","label":"好","options":[],"url":"","style":""}]},"effects":[],"waiting":null}`,
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseExecutionResult(msg); err == nil {
				t.Fatalf("parseExecutionResult(%s) succeeded, want fail-fast", name)
			}
		})
	}
}

// TestParseExecutionResultRejectsOverLongProgressSummary pins where the summary
// ceiling is enforced. The parser is the only real gate — --output-schema
// describes the contract to the model without constraining decoding — and a
// violation caught here is handed back to the same session for a more compact
// restatement. Enforcing it only at the store would instead drop the progress of
// a run that already did its work.
func TestParseExecutionResultRejectsOverLongProgressSummary(t *testing.T) {
	atLimit := strings.Repeat("界", TaskSummaryMaxChars)
	if _, err := parseExecutionResult(progressSummaryResult(atLimit)); err != nil {
		t.Fatalf("parseExecutionResult() at the ceiling error = %v, want it accepted", err)
	}

	over := strings.Repeat("超", TaskSummaryMaxChars+1)
	err := parseExecutionResultErr(t, over)
	for _, want := range []string{"progress_summary", "1001", "1000", "压缩"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to mention %q so the rewrite turn knows what to fix", err, want)
		}
	}
	// The store's own ErrInvalidInput must not leak into the agent contract: the
	// runner routes rewrites on ErrSchemaViolation alone.
	if errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want a contract violation rather than a store input error", err)
	}
}

func progressSummaryResult(progress string) string {
	msg, err := json.Marshal(map[string]any{
		"outcome": "completed", "summary": "已完成",
		"progress_summary": progress, "failure_reason": "",
		"enrichments": []any{}, "effects": []any{}, "question": nil, "waiting": nil,
	})
	if err != nil {
		panic(err)
	}
	return string(msg)
}

func parseExecutionResultErr(t *testing.T, progress string) error {
	t.Helper()
	_, err := parseExecutionResult(progressSummaryResult(progress))
	if err == nil {
		t.Fatal("parseExecutionResult() over the ceiling succeeded, want a rewrite-triggering violation")
	}
	return err
}

// TestParseExecutionResultIgnoresUnknownField pins the other half: a verdict
// that carries everything we consume must not be thrown away because the model
// invented an extra key. The run already happened; losing it over a stray field
// costs a real execution and reports the Task as failed when it was not.
func TestParseExecutionResultIgnoresUnknownField(t *testing.T) {
	msg := `{"outcome":"completed","progress_summary":"","summary":"已回复","failure_reason":"","enrichments":[],"question":null,"effects":[],"waiting":null,"extra":1}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if result.Summary != "已回复" {
		t.Fatalf("summary = %q", result.Summary)
	}
}

// TestParseDirectExecutionResult accepts a verdict where the agent finished the
// work in place, with nothing to ask.
func TestParseDirectExecutionResult(t *testing.T) {
	msg := `{"outcome":"completed","progress_summary":"","summary":"已给自己发提醒","failure_reason":"","enrichments":[],"question":null,"effects":[],"waiting":null}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if result.Outcome != "completed" {
		t.Fatalf("result = %#v", result)
	}
}

// TestParseExecutionFailureNeedsReason keeps the existing fail-fast:
// a failed low-risk verdict must explain why.
func TestParseExecutionFailureNeedsReason(t *testing.T) {
	msg := `{"outcome":"failed","progress_summary":"","summary":"没做成","failure_reason":"","enrichments":[],"question":null,"effects":[],"waiting":null}`
	if _, err := parseExecutionResult(msg); err == nil {
		t.Fatalf("outcome=failed without failure_reason must fail")
	}
}

func TestParseExecutionResultWaiting(t *testing.T) {
	msg := `{"outcome":"waiting","progress_summary":"","summary":"会议仍在进行","failure_reason":"","enrichments":[],"effects":[],"question":null,"waiting":{"scheduled_task_id":42,"wake_at":"2026-07-23T16:30:00+08:00","reason":"稍后检查妙记"}}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if result.Outcome != "waiting" || result.Waiting == nil || result.Waiting.ScheduledTaskID != 42 {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseExecutionResultRejectsUnscheduledWaiting(t *testing.T) {
	msg := `{"outcome":"waiting","progress_summary":"","summary":"稍后再看","failure_reason":"","enrichments":[],"effects":[],"question":null,"waiting":null}`
	if _, err := parseExecutionResult(msg); err == nil {
		t.Fatal("outcome=waiting without a scheduled task must fail")
	}
}

func TestParseExecutionResultNeedsHuman(t *testing.T) {
	msg := `{"outcome":"needs_human","progress_summary":"","summary":"授权页已打开","failure_reason":"","question":{"title":"你点授权了吗","body":"","fields":[{"type":"button","name":"done","label":"已授权","options":[],"url":"","style":"primary"}]},"enrichments":[],"effects":[],"waiting":null}`
	result, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parseExecutionResult() error = %v", err)
	}
	if result.Outcome != "needs_human" || len(result.Question) == 0 {
		t.Fatalf("result = %#v", result)
	}

	noQuestion := `{"outcome":"needs_human","progress_summary":"","summary":"需要人工","failure_reason":"","enrichments":[],"effects":[],"question":null,"waiting":null}`
	if _, err := parseExecutionResult(noQuestion); err == nil {
		t.Fatal("outcome=needs_human without a question must fail")
	}
}

func TestParseExecutionResultAcceptsStringEnrichmentContent(t *testing.T) {
	msg := `{"outcome":"completed","progress_summary":"","summary":"已完成","failure_reason":"","enrichments":[{"kind":"code_link","label":"核心修改","content":"internal/execute/prompt.go"},{"kind":"risk","label":"风险","content":"需要同步前端"}],"effects":[],"question":null,"waiting":null}`
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
		"object content": `{"outcome":"completed","progress_summary":"","summary":"已完成","failure_reason":"","enrichments":[{"kind":"code_link","label":"核心修改","content":{"path":"x"}}],"effects":[],"question":null,"waiting":null}`,
		"array content":  `{"outcome":"completed","progress_summary":"","summary":"已完成","failure_reason":"","enrichments":[{"kind":"risk","label":"风险","content":["需要同步前端"]}],"effects":[],"question":null,"waiting":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseExecutionResult(msg); err == nil {
				t.Fatalf("parseExecutionResult(%s) succeeded, want fail-fast", name)
			}
		})
	}
}

func TestParseExecutionResultRejectsLegacyEnrichmentDetail(t *testing.T) {
	msg := `{"outcome":"completed","progress_summary":"","summary":"已完成","failure_reason":"","enrichments":[{"kind":"code_link","label":"核心修改","detail":"internal/execute/prompt.go"}],"effects":[],"question":null,"waiting":null}`
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
	msg := `{"outcome":"needs_human","progress_summary":"","summary":"已完成最小修复，推送需提权","failure_reason":"","enrichments":[{"kind":"evidence","label":"权限核验","content":"accessLevel=reporter"},{"kind":"memory_citation","label":"Memory sources","content":""}],"effects":[],"question":{"title":"要不要按这个方向继续","body":"","fields":[{"type":"button","name":"go","label":"继续","options":[],"url":"","style":"primary"}]},"waiting":null}`
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
	msg := `{"outcome":"needs_human","progress_summary":"","summary":"已推送 MR","failure_reason":"","enrichments":[{"kind":"merge_request","label":"MR !5","content":"https://example.com/mr/5"},{"kind":"memory_citation","label":"Memory citation","content":""}],"effects":[],"question":{"title":"要不要按这个方向继续","body":"","fields":[{"type":"button","name":"go","label":"继续","options":[],"url":"","style":"primary"}]},"waiting":null}`
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
	msg := `{"outcome":"completed","progress_summary":"","summary":"已完成","failure_reason":"","enrichments":[{"kind":"evidence","label":"证据","content":""}],"effects":[],"question":null,"waiting":null}`
	if _, err := parseExecutionResult(msg); err == nil {
		t.Fatal("blank non-memory enrichment content must still fail-fast")
	}
}

func TestParseExecutionResultRejectsIncompleteEnrichment(t *testing.T) {
	cases := map[string]string{
		"blank kind":      `{"outcome":"completed","progress_summary":"","summary":"已完成","failure_reason":"","enrichments":[{"kind":"","label":"核心修改","content":"x"}],"effects":[],"question":null,"waiting":null}`,
		"blank label":     `{"outcome":"completed","progress_summary":"","summary":"已完成","failure_reason":"","enrichments":[{"kind":"code_link","label":"","content":"x"}],"effects":[],"question":null,"waiting":null}`,
		"missing content": `{"outcome":"completed","progress_summary":"","summary":"已完成","failure_reason":"","enrichments":[{"kind":"code_link","label":"核心修改"}],"effects":[],"question":null,"waiting":null}`,
		"null content":    `{"outcome":"completed","progress_summary":"","summary":"已完成","failure_reason":"","enrichments":[{"kind":"code_link","label":"核心修改","content":null}],"effects":[],"question":null,"waiting":null}`,
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseExecutionResult(msg); err == nil {
				t.Fatalf("parseExecutionResult(%s) succeeded, want fail-fast", name)
			}
		})
	}
}
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
func TestAppendQuestionCardEffectKeepsAgentEffects(t *testing.T) {
	raw, err := appendQuestionCardEffect(json.RawMessage(`[{"kind":"file","title":"调查材料"}]`), &QuestionDelivery{
		MessageID: "om_approval", Target: "飞书私聊 principal",
		Preview: "要不要把结论发到评测群", URL: "http://127.0.0.1:18800/#/work/task/7",
	})
	if err != nil {
		t.Fatalf("appendQuestionCardEffect() error = %v", err)
	}
	var effects []map[string]any
	if err := json.Unmarshal(raw, &effects); err != nil {
		t.Fatalf("decode effects: %v", err)
	}
	if len(effects) != 2 || effects[0]["kind"] != "file" || effects[1]["message_id"] != "om_approval" {
		t.Fatalf("effects = %#v", effects)
	}
}

var errTest = errors.New("group not found")

// TestResumePromptsRequireApprovalPolicy pins the fix for resumed sessions being
// asked to judge approval without the policy to judge against. Both resume
// paths must refuse to build rather than run blind.
func TestResumePromptsRequireApprovalPolicy(t *testing.T) {
	if _, err := buildHumanResumePrompt(testM5SystemPrompt, "  ", "回应", "", testToolCatalog, "", "normal"); err == nil {
		t.Fatal("buildHumanResumePrompt() with blank approval policy = nil error, want error")
	}
	if _, err := buildScheduledResumePrompt(testM5SystemPrompt, "", "等 CI", "", testToolCatalog, "", "normal"); err == nil {
		t.Fatal("buildScheduledResumePrompt() with blank approval policy = nil error, want error")
	}
}

func TestBuildScheduledResumePromptCarriesApprovalPolicy(t *testing.T) {
	prompt, err := buildScheduledResumePrompt(testM5SystemPrompt, "test approval policy", "等 CI 跑完", "", testToolCatalog, "CURRENT_SKILL_CHECK_ONCE", "normal")
	if err != nil {
		t.Fatalf("buildScheduledResumePrompt() error = %v", err)
	}
	for _, want := range []string{"phase=resume_waiting", "CURRENT_SKILL_CHECK_ONCE", "等 CI 跑完", "BEGIN_APPROVAL_POLICY", "test approval policy"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("scheduled resume prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildHumanResumePrompt(t *testing.T) {
	prompt, err := buildHumanResumePrompt(testM5SystemPrompt, "test approval policy", "我已确认授权，请继续", "", testToolCatalog, "CURRENT_HUMAN_SKILL", "normal")
	if err != nil {
		t.Fatalf("buildHumanResumePrompt() error = %v", err)
	}
	for _, want := range []string{
		"我已确认授权，请继续",
		"phase=resume_human",
		"CURRENT_HUMAN_SKILL",
		"BEGIN_APPROVAL_POLICY",
		"test approval policy",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("human resume prompt missing %q:\n%s", want, prompt)
		}
	}
}
func TestBuildExecutionPrompt(t *testing.T) {
	task := &domain.Task{
		ID: 11, Title: "更新周报", ActionType: "doc_write",
		SourcePayload: frozenTestContent(`{"steps":["update"]}`, `{}`),
	}
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "修改文件需要审批。", task, "", testToolCatalog, "", "", "", nil))
	if err != nil {
		t.Fatalf("buildExecutionPrompt() error = %v", err)
	}
	for _, want := range []string{
		"phase=execute",
		"BEGIN_APPROVAL_POLICY",
		"修改文件需要审批。",
		"BEGIN_TASK_CONTEXT",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q", want)
		}
	}
}

// 共享记忆非空时，execution prompt 应在 TASK_CONTEXT 之前包含 BEGIN_SHARED_MEMORY 标记
// 与内容；为空时不包含。
func TestBuildExecutionPromptIncludesSharedMemory(t *testing.T) {
	task := &domain.Task{
		ID: 11, Title: "更新周报", ActionType: "doc_write",
		SourcePayload: frozenTestContent(`{"steps":["update"]}`, `{}`),
	}
	empty, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "只读不审批。", task, "", testToolCatalog, "", "", "", nil))
	if err != nil {
		t.Fatalf("buildExecutionPrompt() error = %v", err)
	}
	if strings.Contains(empty, "BEGIN_SHARED_MEMORY") {
		t.Fatalf("empty shared memory must not inject block:\n%s", empty)
	}
	prompt, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "只读不审批。", task, "", testToolCatalog, "周报模板固定用飞书文档 xxx", "", "", nil))
	if err != nil {
		t.Fatalf("buildExecutionPrompt() error = %v", err)
	}
	for _, want := range []string{"BEGIN_SHARED_MEMORY", "周报模板固定用飞书文档 xxx", "可信"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("execution prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Index(prompt, "BEGIN_SHARED_MEMORY") >= strings.Index(prompt, "BEGIN_TASK_CONTEXT") {
		t.Fatalf("shared memory block must precede TASK_CONTEXT:\n%s", prompt)
	}
}

func TestBuildExecutionPromptRequiresApprovalPolicy(t *testing.T) {
	task := &domain.Task{ID: 14, Title: "x", ActionType: "doc_write", SourcePayload: frozenTestContent(`{}`, `{}`)}
	if _, err := buildExecutionPrompt(testExecutionPromptInput(testM5SystemPrompt, "", task, "", testToolCatalog, "", "", "", nil)); err == nil {
		t.Fatal("empty approval policy must fail")
	}
}

// TestExecutionRoutingReadOnlyVsMutation documents the two execution outcomes: a
// read-only investigation finishes in place, while an intended side effect the
// policy gates parks behind a question carrying its exact content.
func TestExecutionRoutingReadOnlyVsMutation(t *testing.T) {
	readOnly := `{"outcome":"completed","progress_summary":"","summary":"已读日志得出结论","failure_reason":"","enrichments":[],"question":null,"effects":[],"waiting":null}`
	low, err := parseExecutionResult(readOnly)
	if err != nil {
		t.Fatalf("read-only parse error = %v", err)
	}
	if low.Outcome != "completed" {
		t.Fatalf("read-only investigate should finish in place: %#v", low)
	}
	if _, err := ParseQuestion(low.Question); err == nil {
		t.Fatalf("read-only investigate must not ask anything: %s", low.Question)
	}

	mutation := `{"outcome":"needs_human","progress_summary":"","summary":"查证中需要发消息给对方","failure_reason":"","enrichments":[],"question":{"title":"要向张三发这条确认消息吗","body":"你好，关于登录超时想确认一下……","fields":[{"type":"button","name":"send","label":"发出去","options":[],"url":"","style":"primary"},{"type":"button","name":"skip","label":"先不发","options":[],"url":"","style":"danger"}]},"effects":[],"waiting":null}`
	high, err := parseExecutionResult(mutation)
	if err != nil {
		t.Fatalf("mutation parse error = %v", err)
	}
	question, err := ParseQuestion(high.Question)
	if err != nil {
		t.Fatalf("high-risk investigate should park with an answerable question: %v", err)
	}
	if !strings.Contains(question.Body, "登录超时") {
		t.Fatalf("question must carry the exact text it wants to send: %#v", question)
	}
}

// TestQuestionSnapshotRebuildsCardFromStoredResult pins the round trip the card
// depends on: execution_result is the only source, so the card can be
// re-rendered word for word after the answer instead of being read back from
// Feishu, whose message API returns a node tree card/update refuses to accept.
func TestQuestionSnapshotRebuildsCardFromStoredResult(t *testing.T) {
	session := "thread-9"
	run := &domain.ExecutionRun{
		ID: 21, ActionType: "summary_post", Sandbox: "danger-full-access",
		Status: "needs_human", CodexSessionID: &session,
		Output: datatypes.JSON(`{"outcome":"needs_human","summary":"已核对三份数据","question":{"title":"要把结论发到评测群吗","body":"拟发送：通过率 92%","fields":[{"type":"button","name":"send","label":"发出去","options":[],"url":"","style":"primary"}]},"enrichments":[],"effects":[],"waiting":null}`),
	}
	summary := "已核对三份数据"
	run.Summary = &summary
	encoded, err := json.Marshal(runResultPayload(run, nil))
	if err != nil {
		t.Fatalf("marshal run result: %v", err)
	}
	task := &domain.Task{ID: 7, Title: "评测结论同步", Version: 12, ExecutionResult: datatypes.JSON(encoded)}

	notice, err := questionSnapshot(task)
	if err != nil {
		t.Fatalf("questionSnapshot() error = %v", err)
	}
	if notice.TaskID != 7 || notice.RunID != 21 || notice.Version != 12 {
		t.Fatalf("notice identity = %#v", notice)
	}
	if notice.Question.Title != "要把结论发到评测群吗" || len(notice.Question.Fields) != 1 {
		t.Fatalf("notice question = %#v", notice.Question)
	}
	if notice.Summary != summary {
		t.Fatalf("notice summary = %q", notice.Summary)
	}
}

// TestQuestionSnapshotFailsWithoutAnAnswerableQuestion keeps a Task from being
// parked behind a card nobody can act on.
func TestQuestionSnapshotFailsWithoutAnAnswerableQuestion(t *testing.T) {
	task := &domain.Task{
		ID: 7, Version: 3,
		ExecutionResult: datatypes.JSON(`{"stage":"executed","summary":"done","source_run_id":21}`),
	}
	if _, err := questionSnapshot(task); err == nil {
		t.Fatal("questionSnapshot() without a question must fail")
	}
}
