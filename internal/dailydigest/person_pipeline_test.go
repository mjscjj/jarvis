package dailydigest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type pipelineSummaryRunner struct {
	mu      sync.Mutex
	started chan string
	release chan struct{}
	calls   []string
}

func (r *pipelineSummaryRunner) RunTextSandbox(_ context.Context, prompt, _ string) (string, error) {
	stage := ""
	switch {
	case strings.Contains(prompt, "synthesis agent"):
		stage = "synthesis"
	case strings.Contains(prompt, "- domain: feishu_work"):
		stage = "feishu"
	case strings.Contains(prompt, "- domain: engineering_execution"):
		stage = "engineering"
	default:
		return "", fmt.Errorf("unknown prompt stage")
	}
	r.mu.Lock()
	r.calls = append(r.calls, stage)
	r.mu.Unlock()
	if stage != "synthesis" {
		r.started <- stage
		<-r.release
	}
	switch stage {
	case "feishu":
		return collectorFixture(
			"feishu_work",
			[]string{"messages_threads", "documents", "meetings_minutes"},
			"feishu:meeting:m1",
		), nil
	case "engineering":
		return collectorFixture(
			"engineering_execution",
			[]string{"agent_sessions", "mrs_reviews", "commits_delivery"},
			"engineering:mr:1",
		), nil
	default:
		return `{
			"summary":"【会议与妙记】\n- 10:00 评审会：确定统一链路\n【今日结论】\n- 交付统一链路\n【按项目变化】\n- Jarvis：Activity → Output → Observed Outcome\n【决策与承诺】\n- 无明确记录\n【风险与阻塞】\n- 无明确记录\n【数据覆盖】\n- 三类来源均已返回",
			"work_item_count":1,
			"evidence_ids":["feishu:meeting:m1","engineering:mr:1"],
			"meetings":[{"evidence_id":"feishu:meeting:m1","summary":"10:00 评审会：确定统一链路"}]
		}`, nil
	}
}

func collectorFixture(domain string, scopes []string, evidenceID string) string {
	counts := []int{1, 0, 0}
	sourceKind := "artifact"
	if domain == "feishu_work" {
		counts = []int{0, 0, 1}
		sourceKind = "meeting"
	}
	return fmt.Sprintf(`{
		"domain":%q,
		"identity_filters":["ou_me","me@example.com"],
		"window":{"start":"2026-07-23T00:00:00+08:00","end":"2026-07-23T18:00:00+08:00","cutoff":"2026-07-23T18:00:00+08:00","timezone":"Asia/Shanghai"},
		"status":"complete",
		"coverage":[
			{"scope":%q,"query_or_cursor":"q1","status":%q,"count":%d,"truncated":false},
			{"scope":%q,"query_or_cursor":"q2","status":"empty","count":0,"truncated":false},
			{"scope":%q,"query_or_cursor":"q3","status":%q,"count":%d,"truncated":false}
		],
		"evidence":[{
			"evidence_id":%q,
			"domain":%q,
			"source_kind":%q,
			"source_id":"1",
			"occurred_at":"2026-07-23T10:00:00+08:00",
			"actor_identity":"ou_me",
			"subject":"统一链路",
			"output":"产物",
			"observed_outcome":"已验证",
			"attribution":"direct",
			"strength":"primary"
		}],
		"gaps":[]
	}`,
		domain,
		scopes[0], statusForCount(counts[0]), counts[0],
		scopes[1],
		scopes[2], statusForCount(counts[2]), counts[2],
		evidenceID, domain, sourceKind,
	)
}

func statusForCount(count int) string {
	if count == 0 {
		return "empty"
	}
	return "complete"
}

func TestPersonGenerateRunsCollectorsInParallelThenSynthesizes(t *testing.T) {
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
		`CREATE TABLE project_event (id INTEGER PRIMARY KEY, occurred_at DATETIME)`,
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
	runner := &pipelineSummaryRunner{
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	generator := &personGenerator{
		db: db, runner: runner, location: location,
		principalOpenID: "ou_me", gitAuthor: "me@example.com",
		repoRoot: "/workspace", skillText: "SKILL", sandbox: "danger-full-access",
	}
	type generateResult struct {
		result *personGenerateResult
		err    error
	}
	done := make(chan generateResult, 1)
	go func() {
		result, generateErr := generator.Generate(
			context.Background(), "2026-07-23", start, start.AddDate(0, 0, 1), start.Add(18*time.Hour),
		)
		done <- generateResult{result: result, err: generateErr}
	}()

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case stage := <-runner.started:
			seen[stage] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("collectors did not start concurrently; seen=%v", seen)
		}
	}
	close(runner.release)
	generated := <-done
	if generated.err != nil {
		t.Fatalf("generate: %v", generated.err)
	}
	if generated.result.SourceCount != 2 {
		t.Fatalf("source_count=%d, want 2 evidence cards", generated.result.SourceCount)
	}
	for _, source := range []string{"jarvis_internal", "feishu_work", "engineering_execution"} {
		if _, ok := generated.result.Coverage[source]; !ok {
			t.Fatalf("coverage missing %q: %#v", source, generated.result.Coverage)
		}
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 3 || runner.calls[2] != "synthesis" {
		t.Fatalf("runner calls=%v, want two collectors then synthesis", runner.calls)
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
		`CREATE TABLE project_event (id INTEGER PRIMARY KEY, occurred_at DATETIME)`,
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
		 (1,'历史开放 Todo','need_info','firm',1),
		 (2,'当天 Todo','confirmed','firm',0)`,
	).Error; err != nil {
		t.Fatalf("insert todos: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO todo_event(id,todo_id,to_status,actor,detail,snapshot,created_at)
		 VALUES (20,2,'confirmed','m4','{}','{"title":"事件时标题","project_id":7,"commitment_strength":"firm","leader_assigned":false,"source_quote":"我会完成","context":"冻结背景"}',?)`,
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
