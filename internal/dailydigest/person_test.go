package dailydigest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type staticPersonSummaryRunner struct {
	output        string
	prompt        string
	sandbox       string
	workspaceRoot string
}

func (r *staticPersonSummaryRunner) RunTextSandbox(
	_ context.Context,
	prompt, sandbox string,
) (string, error) {
	r.prompt = prompt
	r.sandbox = sandbox
	return r.output, nil
}

func (r *staticPersonSummaryRunner) RunTextSandboxAt(
	_ context.Context,
	prompt, sandbox, workspaceRoot string,
) (string, error) {
	r.prompt = prompt
	r.sandbox = sandbox
	r.workspaceRoot = workspaceRoot
	return r.output, nil
}

func TestCapRunes(t *testing.T) {
	t.Parallel()
	if got := capRunes("abc", 5); got != "abc" {
		t.Fatalf("short = %q", got)
	}
	if got := capRunes("一二三四五六", 3); got != "一二三…" {
		t.Fatalf("capped = %q", got)
	}
}

func TestValidatePersonReportRequiresDailyReportHeadings(t *testing.T) {
	t.Parallel()
	report := validPersonReport("2026-07-25")
	if err := validatePersonReport(report, "2026-07-25"); err != nil {
		t.Fatalf("validate report: %v", err)
	}
	broken := strings.Replace(report, "## 关联、洞察与其他发现", "## 洞察", 1)
	if err := validatePersonReport(broken, "2026-07-25"); err == nil {
		t.Fatal("accepted report without the required insight heading")
	}
	if err := validatePersonReport(report, "2026-07-24"); err == nil {
		t.Fatal("accepted report for a different date")
	}
}

func TestInspectPersonWorkspaceProjectsMarkdownControlFields(t *testing.T) {
	t.Parallel()
	dayDir := t.TempDir()
	runID := "daily-panorama-test"
	writeTestFile(t, filepath.Join(dayDir, "00-context.md"), `# Context

## Run log
- Run ID: `+runID+`

## Coverage
- Jarvis: complete
- Feishu: partial
- Engineering: empty
`)
	writeTestFile(t, filepath.Join(dayDir, "10-evidence-jarvis.md"), evidenceFixture("JARVIS-message-1"))
	writeTestFile(t, filepath.Join(dayDir, "20-evidence-feishu.md"), evidenceFixture("FEISHU-meeting-1"))
	writeTestFile(t, filepath.Join(dayDir, "30-evidence-engineering.md"), evidenceFixture(""))

	count, coverage, err := inspectPersonWorkspace(dayDir, runID)
	if err != nil {
		t.Fatalf("inspect workspace: %v", err)
	}
	if count != 2 {
		t.Fatalf("source count = %d, want 2", count)
	}
	if coverage["jarvis_internal"].Status != "complete" ||
		coverage["feishu_work"].Status != "partial" ||
		coverage["engineering_execution"].Status != "empty" {
		t.Fatalf("coverage = %#v", coverage)
	}
	if coverage["feishu_work"].Count != 1 {
		t.Fatalf("Feishu count = %d, want 1", coverage["feishu_work"].Count)
	}
}

func TestReadFreshPersonReportRejectsUnchangedCanonicalFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "99-report.md")
	writeTestFile(t, path, validPersonReport("2026-07-25"))
	before, err := snapshotReport(path)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, err := readFreshPersonReport(path, "2026-07-25", before); err == nil {
		t.Fatal("accepted a canonical report that was not refreshed")
	}
}

func TestBuildSkillPromptCarriesWorkspaceAndParallelContract(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	day, err := time.ParseInLocation("2006-01-02", "2026-07-25", loc)
	if err != nil {
		t.Fatalf("parse day: %v", err)
	}
	generator := &personGenerator{
		location:        loc,
		principalOpenID: "ou_me",
		gitAuthor:       "me@example.com",
		repoRoot:        "/workspace-local",
		workspaceRoot:   "/workspace-local/jarvis",
		skillDir:        "/workspace-local/jarvis/.agents/skills/summarize-person-day",
		skillText:       "LEAN SKILL ENTRY",
	}
	prompt := generator.buildSkillPrompt(
		"run-1",
		"2026-07-25",
		day,
		day.Add(18*time.Hour),
		day.Add(18*time.Hour),
		"/workspace-local/jarvis/data/personal-daily/2026-07-25",
		"/workspace-local/jarvis/data/personal-daily/2026-07-25/10-evidence-jarvis.md",
		false,
	)
	for _, expected := range []string{
		"LEAN SKILL ENTRY",
		"Run ID: run-1",
		"Principal open_id: ou_me",
		"Engineering repository discovery root: /workspace-local",
		"只启动两个并行 subagent",
		"collector 禁止再派生任何 agent",
		"99-report.md",
		"# 我的日报 · YYYY-MM-DD",
		"九个 `##` 导航章节",
		"不要输出 JSON DTO",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt missing %q:\n%s", expected, prompt)
		}
	}
	for _, forbidden := range []string{"50-verification.md", "90-report-draft.md", "轮数不固定"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt contains removed stage %q:\n%s", forbidden, prompt)
		}
	}
}

func TestLoadPersonSummarySkillRequiresWholePackage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "SKILL.md"), "main workflow")
	if _, err := loadPersonSummarySkill(dir); err == nil {
		t.Fatal("loaded incomplete person summary skill")
	}

	writeTestFile(t, filepath.Join(dir, "references", "context-and-capabilities.md"), "capabilities")
	writeTestFile(t, filepath.Join(dir, "references", "storage-and-report.md"), "storage")
	writeTestFile(t, filepath.Join(dir, "references", "channel-methods.md"), "channels")
	scriptPath := filepath.Join(dir, "scripts", "init-day.sh")
	writeTestFile(t, scriptPath, "#!/usr/bin/env bash\nexit 0\n")
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("chmod initializer: %v", err)
	}
	text, err := loadPersonSummarySkill(dir)
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	if text != "main workflow" {
		t.Fatalf("loaded skill text = %q, want only SKILL.md", text)
	}
}

func TestRenderJarvisEvidenceCollapsesRepeatedTodoAndTaskEvents(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	start, err := time.ParseInLocation("2006-01-02", "2026-07-25", location)
	if err != nil {
		t.Fatalf("parse day: %v", err)
	}
	generator := &personGenerator{location: location, principalOpenID: "ou_me"}
	baseline := &personBaseline{
		TodoEvents: []baselineTodoEvent{
			{EventID: 1, TodoID: 9, OccurredAt: "2026-07-25T09:00:00+08:00", FromStatus: "extracted", ToStatus: "materialized", Actor: "materializer", Title: "同一事项", ContextSnapshot: "same large context"},
			{EventID: 2, TodoID: 9, OccurredAt: "2026-07-25T09:01:00+08:00", FromStatus: "materialized", ToStatus: "observing", Actor: "m5", Title: "同一事项", ContextSnapshot: "same large context"},
		},
		TaskEvents: []baselineTaskEvent{
			{EventID: 3, TaskID: 10, OccurredAt: "2026-07-25T09:02:00+08:00", EventType: "created", ToStatus: "pending", ActorType: "m5", Title: "同一任务", Background: "same background"},
			{EventID: 4, TaskID: 10, OccurredAt: "2026-07-25T09:03:00+08:00", EventType: "execution_succeeded", FromStatus: "running", ToStatus: "done", ActorType: "m5", Title: "同一任务", Background: "same background"},
		},
		ExecutionRuns: []baselineExecutionRun{
			{RunID: 5, TaskID: 10, OccurredAt: "2026-07-25T09:04:00+08:00", Status: "failed", ActionType: "analysis", Title: "同一任务", Summary: "first attempt"},
			{RunID: 6, TaskID: 10, OccurredAt: "2026-07-25T09:05:00+08:00", Status: "succeeded", ActionType: "analysis", Title: "同一任务", Summary: "second attempt"},
		},
	}
	rendered := generator.renderJarvisEvidence(
		"2026-07-25",
		start,
		start.Add(10*time.Hour),
		start.Add(10*time.Hour),
		baseline,
	)
	if strings.Count(rendered, "### JARVIS-todo-9 —") != 1 {
		t.Fatalf("todo lifecycle was not collapsed:\n%s", rendered)
	}
	if strings.Count(rendered, "### JARVIS-task-10 —") != 1 {
		t.Fatalf("task lifecycle was not collapsed:\n%s", rendered)
	}
	if strings.Count(rendered, "### JARVIS-execution-task-10 —") != 1 {
		t.Fatalf("execution runs were not collapsed by task:\n%s", rendered)
	}
	if strings.Count(rendered, "same large context") != 1 ||
		strings.Count(rendered, "same background") != 1 {
		t.Fatalf("repeated context leaked into compact seed:\n%s", rendered)
	}
}

func evidenceFixture(evidenceID string) string {
	var evidence string
	if evidenceID != "" {
		evidence = "\n### " + evidenceID + " — fixture\n- Source kind: fixture\n"
	}
	return "# Evidence fixture\n\n## Coverage\n- fixture: complete\n\n## Evidence\n" + evidence + "\n## Gaps\n- None\n"
}

func validPersonReport(date string) string {
	return "# 我的日报 · " + date + `

## 今日数据
数据。
## 今天的会议
会议。
## 消息与协作
消息。
## 项目与工作进展
进展。
## 已完成事项
完成。
## 待讨论事项
讨论。
## 后续计划
计划。
## 关联、洞察与其他发现
发现。
## 数据说明
说明。`
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
