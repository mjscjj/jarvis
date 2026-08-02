package dailydigest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type workspacePersonRunner struct {
	t             *testing.T
	calls         int
	prompt        string
	sandbox       string
	workspaceRoot string
}

func (r *workspacePersonRunner) RunTextSandbox(
	_ context.Context,
	_, _ string,
) (string, error) {
	return "", fmt.Errorf("person panorama must use a workspace-rooted run")
}

func (r *workspacePersonRunner) RunTextSandboxAt(
	_ context.Context,
	prompt, sandbox, workspaceRoot string,
) (string, error) {
	r.calls++
	r.prompt = prompt
	r.sandbox = sandbox
	r.workspaceRoot = workspaceRoot
	runID := promptLineValue(prompt, "- Run ID: ")
	if runID == "" {
		return "", fmt.Errorf("prompt missing run ID")
	}
	dayDir := filepath.Join(workspaceRoot, "data", "personal-daily", "2026-07-23")
	writeTestFile(r.t, filepath.Join(dayDir, "00-context.md"), `# Daily context — 2026-07-23

## Run log
- Run ID: `+runID+`

## Coverage
- Jarvis: empty
- Feishu: complete
- Engineering: complete
`)
	writeTestFile(r.t, filepath.Join(dayDir, "20-evidence-feishu.md"), evidenceFixture("FEISHU-meeting-m1"))
	writeTestFile(r.t, filepath.Join(dayDir, "30-evidence-engineering.md"), evidenceFixture("ENG-mr-1"))
	writeTestFile(r.t, filepath.Join(dayDir, "99-report.md"), validPersonReport("2026-07-23"))
	return "完成：" + filepath.Join(dayDir, "99-report.md"), nil
}

func TestPersonGenerateRunsOneWorkspaceRootedSkillAndReadsCanonicalMarkdown(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE message (id INTEGER PRIMARY KEY, sender_open_id TEXT, create_time INTEGER)`,
		`CREATE TABLE todo_event (id INTEGER PRIMARY KEY, created_at DATETIME)`,
		`CREATE TABLE task_event (id INTEGER PRIMARY KEY, occurred_at DATETIME)`,
		`CREATE TABLE execution_run (
			id INTEGER PRIMARY KEY, task_id INTEGER, started_at DATETIME, finished_at DATETIME,
			status TEXT, action_type TEXT, summary TEXT, error_detail TEXT,
			commit_sha TEXT, merge_request_url TEXT, codex_session_id TEXT
		)`,
		`CREATE TABLE fact (
			id INTEGER PRIMARY KEY AUTOINCREMENT, subject_type TEXT NOT NULL, subject_id INTEGER NOT NULL,
			description TEXT NOT NULL, occurred_at DATETIME NOT NULL,
			source_kind TEXT, source_id INTEGER, created_at DATETIME
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	start, err := time.ParseInLocation("2006-01-02", "2026-07-23", location)
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	skillDir, err := filepath.Abs("../../.agents/skills/summarize-person-day")
	if err != nil {
		t.Fatalf("resolve skill dir: %v", err)
	}
	workspaceRoot := t.TempDir()
	runner := &workspacePersonRunner{t: t}
	generator := &personGenerator{
		db: db, runner: runner, location: location,
		principalOpenID: "ou_me", gitAuthor: "me@example.com",
		repoRoot: "/workspace", workspaceRoot: workspaceRoot,
		skillDir: skillDir, skillText: "LEAN SKILL ENTRY",
		sandbox: "danger-full-access",
	}

	result, err := generator.Generate(
		context.Background(),
		"2026-07-23",
		start,
		start.AddDate(0, 0, 1),
		start.Add(18*time.Hour),
	)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("workspace runner calls = %d, want 1", runner.calls)
	}
	if runner.workspaceRoot != workspaceRoot || runner.sandbox != "danger-full-access" {
		t.Fatalf("runner workspace=%q sandbox=%q", runner.workspaceRoot, runner.sandbox)
	}
	for _, expected := range []string{"LEAN SKILL ENTRY", "只启动两个并行 subagent", "# 我的日报 · YYYY-MM-DD", "99-report.md"} {
		if !strings.Contains(runner.prompt, expected) {
			t.Fatalf("prompt missing %q:\n%s", expected, runner.prompt)
		}
	}
	if result.SourceCount != 2 {
		t.Fatalf("source count = %d, want 2", result.SourceCount)
	}
	if result.Summary != validPersonReport("2026-07-23") {
		t.Fatalf("summary = %q", result.Summary)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, "data", "personal-daily", "2026-07-23", "10-evidence-jarvis.md")); err != nil {
		t.Fatalf("Jarvis evidence missing: %v", err)
	}
}

func TestLoadBaselineUsesDayEventsAndIgnoresHistoricalOpenRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE message (id INTEGER PRIMARY KEY, message_id TEXT, sender_open_id TEXT, create_time INTEGER)`,
		`CREATE TABLE todo (
			id INTEGER PRIMARY KEY, title TEXT, status TEXT, commitment_strength TEXT,
			is_leader_assigned BOOLEAN, source_quote TEXT, context_snapshot TEXT,
			resolution TEXT, project_id INTEGER
		)`,
		`CREATE TABLE todo_event (
			id INTEGER PRIMARY KEY, todo_id INTEGER, from_status TEXT, to_status TEXT,
			actor TEXT, detail TEXT, snapshot TEXT, created_at DATETIME
		)`,
		`CREATE TABLE task (id INTEGER PRIMARY KEY, title TEXT, background TEXT, project_id INTEGER)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY, task_id INTEGER, event_type TEXT, from_status TEXT,
			to_status TEXT, actor_type TEXT, detail TEXT, occurred_at DATETIME
		)`,
		`CREATE TABLE execution_run (
			id INTEGER PRIMARY KEY, task_id INTEGER, started_at DATETIME, finished_at DATETIME,
			status TEXT, action_type TEXT, summary TEXT, error_detail TEXT,
			commit_sha TEXT, merge_request_url TEXT, codex_session_id TEXT
		)`,
		`CREATE TABLE fact (
			id INTEGER PRIMARY KEY AUTOINCREMENT, subject_type TEXT NOT NULL, subject_id INTEGER NOT NULL,
			description TEXT NOT NULL, occurred_at DATETIME NOT NULL,
			source_kind TEXT, source_id INTEGER, created_at DATETIME
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	start, err := time.ParseInLocation("2006-01-02", "2026-07-23", location)
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO todo(id,title,status,commitment_strength,is_leader_assigned) VALUES
		 (1,'历史观察 Todo','observing','firm',1),
		 (2,'当天 Todo','materialized','firm',0)`,
	).Error; err != nil {
		t.Fatalf("insert todos: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO todo_event(id,todo_id,from_status,to_status,actor,detail,snapshot,created_at)
		 VALUES (20,2,'extracted','materialized','materializer','{}','{"title":"事件时标题","project_id":7,"commitment_strength":"firm","leader_assigned":false,"source_quote":"我会完成","context":"冻结背景"}',?)`,
		start.Add(9*time.Hour),
	).Error; err != nil {
		t.Fatalf("insert todo event: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO task(id,title,background) VALUES (1,'历史开放 Task','{}'),(2,'当天 Task','{}')`,
	).Error; err != nil {
		t.Fatalf("insert tasks: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO task_event(id,task_id,event_type,to_status,actor_type,detail,occurred_at)
		 VALUES (30,2,'execution_succeeded','done','m5','{}',?)`,
		start.Add(10*time.Hour),
	).Error; err != nil {
		t.Fatalf("insert task event: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO execution_run(id,task_id,started_at,finished_at,status,action_type,summary,commit_sha)
		 VALUES
		 (40,2,?,?, 'succeeded','code_change','次日才完成','future-commit'),
		 (41,2,?,?, 'succeeded','code_change','当天完成','today-commit')`,
		start.Add(11*time.Hour), start.Add(26*time.Hour),
		start.Add(-2*time.Hour), start.Add(2*time.Hour),
	).Error; err != nil {
		t.Fatalf("insert execution run: %v", err)
	}
	generator := &personGenerator{db: db, location: location, principalOpenID: "ou_me"}
	baseline, err := generator.loadBaseline(context.Background(), start, start.Add(18*time.Hour))
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	if len(baseline.TodoEvents) != 1 ||
		baseline.TodoEvents[0].TodoID != 2 ||
		baseline.TodoEvents[0].Title != "事件时标题" ||
		baseline.TodoEvents[0].ProjectBinding != "project_id:7" {
		t.Fatalf("todo events = %#v", baseline.TodoEvents)
	}
	if len(baseline.TaskEvents) != 1 || baseline.TaskEvents[0].TaskID != 2 {
		t.Fatalf("task events = %#v", baseline.TaskEvents)
	}
	if len(baseline.ExecutionRuns) != 2 ||
		baseline.ExecutionRuns[1].Status != "running_at_cutoff" ||
		baseline.ExecutionRuns[1].Summary != "" ||
		baseline.ExecutionRuns[1].Commit != "" {
		t.Fatalf("future run result leaked across cutoff: %#v", baseline.ExecutionRuns)
	}
	if baseline.ExecutionRuns[0].OccurredAt != start.Add(2*time.Hour).Format(time.RFC3339) ||
		baseline.ExecutionRuns[0].Summary != "当天完成" {
		t.Fatalf("cross-day completion not attributed to finish time: %#v", baseline.ExecutionRuns[0])
	}
}

func promptLineValue(prompt, prefix string) string {
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
