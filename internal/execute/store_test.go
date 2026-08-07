package execute

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateTaskFilter(t *testing.T) {
	if err := ValidateTaskFilter(TaskFilter{
		Statuses: []string{"pending", "executing", "waiting", "needs_human", "awaiting_approval", "done", "failed"},
		Page:     1, PageSize: 20,
	}); err != nil {
		t.Fatalf("ValidateTaskFilter() error = %v", err)
	}
	for _, filter := range []TaskFilter{
		{Page: 0, PageSize: 20},
		{Page: 1, PageSize: 101},
		{Statuses: []string{"nonsense"}, Page: 1, PageSize: 20},
	} {
		if err := ValidateTaskFilter(filter); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("ValidateTaskFilter(%#v) error = %v", filter, err)
		}
	}
}

func TestParseStatuses(t *testing.T) {
	statuses, err := ParseStatuses("pending,waiting,needs_human,awaiting_approval,done,pending")
	if err != nil {
		t.Fatalf("ParseStatuses() error = %v", err)
	}
	want := []string{"pending", "waiting", "needs_human", "awaiting_approval", "done"}
	if len(statuses) != len(want) {
		t.Fatalf("statuses = %v", statuses)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("statuses[%d] = %q, want %q", i, statuses[i], want[i])
		}
	}
	if _, err := ParseStatuses("pending,unknown"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ParseStatuses() error = %v", err)
	}
}

func TestRunViewIncludesFullPrompt(t *testing.T) {
	prompt := strings.Repeat("完整原始提示词\n", 10_000)
	view := runView(&domain.ExecutionRun{ID: 1, Prompt: prompt})
	if view.Prompt != prompt {
		t.Fatalf("prompt length = %d, want %d", len(view.Prompt), len(prompt))
	}
}

func TestNeedsHumanSourceRunID(t *testing.T) {
	got, err := needsHumanSourceRunID([]byte(`{"outcome":"needs_human","source_run_id":135}`))
	if err != nil || got != 135 {
		t.Fatalf("needsHumanSourceRunID() = %d, err = %v", got, err)
	}
	for name, raw := range map[string][]byte{
		"empty":       nil,
		"wrong state": []byte(`{"outcome":"failed","source_run_id":135}`),
		"missing run": []byte(`{"outcome":"needs_human"}`),
		"malformed":   []byte(`{`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := needsHumanSourceRunID(raw); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("needsHumanSourceRunID(%q) error = %v", raw, err)
			}
		})
	}
}

func TestNeedsHumanPauseAndResumePersistsSameSession(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY,
			status TEXT NOT NULL,
			execution_result TEXT,
			execution_supplements TEXT,
			version INTEGER NOT NULL,
			updated_at DATETIME
		)`,
		`CREATE TABLE execution_run (
			id INTEGER PRIMARY KEY,
			task_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			codex_session_id TEXT
		)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			task_version INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			from_status TEXT,
			to_status TEXT NOT NULL,
			actor_type TEXT NOT NULL,
			actor_ref TEXT,
			run_id INTEGER,
			detail TEXT,
			occurred_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(task_id, task_version)
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	const (
		taskID      = uint64(54)
		runID       = uint64(135)
		taskVersion = int32(5)
	)
	if err := db.Exec(
		"INSERT INTO task(id, status, version) VALUES (?, ?, ?)",
		taskID, "executing", taskVersion,
	).Error; err != nil {
		t.Fatalf("create Task: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO execution_run(id, task_id, status, codex_session_id) VALUES (?, ?, ?, ?)",
		runID, taskID, "needs_human", "session-original",
	).Error; err != nil {
		t.Fatalf("create execution run: %v", err)
	}
	result := []byte(fmt.Sprintf(
		`{"outcome":"needs_human","source_run_id":%d,"summary":"授权页已打开","needs_followup":"请确认授权"}`,
		runID,
	))
	if _, err := store.MarkNeedsHuman(t.Context(), taskID, taskVersion, runID, result); err != nil {
		t.Fatalf("MarkNeedsHuman() error = %v", err)
	}

	var parked domain.Task
	if err := db.First(&parked, taskID).Error; err != nil {
		t.Fatalf("load parked Task: %v", err)
	}
	if parked.Status != "needs_human" || parked.Version != 6 {
		t.Fatalf("parked Task status/version = %s/%d", parked.Status, parked.Version)
	}

	claim, err := store.ClaimNeedsHuman(t.Context(), taskID, parked.Version, "已确认授权，请继续", "web")
	if err != nil {
		t.Fatalf("ClaimNeedsHuman() error = %v", err)
	}
	if claim.SourceRunID != runID || claim.Version != 7 || claim.Response != "已确认授权，请继续" {
		t.Fatalf("claim = %#v", claim)
	}
	var resumed domain.Task
	if err := db.First(&resumed, taskID).Error; err != nil {
		t.Fatalf("load resumed Task: %v", err)
	}
	if resumed.Status != "executing" || resumed.Version != 7 {
		t.Fatalf("resumed Task status/version = %s/%d", resumed.Status, resumed.Version)
	}
	if !strings.Contains(string(resumed.ExecutionSupplements), "已确认授权，请继续") {
		t.Fatalf("execution supplements = %s", resumed.ExecutionSupplements)
	}
	var events []domain.TaskEvent
	if err := db.Where("task_id = ?", taskID).Order("task_version").Find(&events).Error; err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(events) != 2 ||
		events[0].EventType != "human_input_requested" ||
		events[1].EventType != "human_response_received" ||
		events[1].RunID == nil || *events[1].RunID != runID {
		t.Fatalf("events = %#v", events)
	}
}

func TestCanonicalJSONObject(t *testing.T) {
	result, err := canonicalJSONObject([]byte(`{"summary":"done","count":1}`))
	if err != nil {
		t.Fatalf("canonicalJSONObject() error = %v", err)
	}
	if string(result) != `{"count":1,"summary":"done"}` {
		t.Fatalf("result = %s", result)
	}
	for _, raw := range [][]byte{nil, []byte(`[]`), []byte(`{}`), []byte(`{} {}`)} {
		if _, err := canonicalJSONObject(raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("canonicalJSONObject(%s) error = %v", raw, err)
		}
	}
}

func TestNewStoreRejectsNilDB(t *testing.T) {
	if _, err := NewStore(nil); err == nil {
		t.Fatal("NewStore(nil) succeeded")
	}
}

func TestTaskTimeWindowsAndPendingOrderUseCreatedAt(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE task (
		id INTEGER PRIMARY KEY,
		status TEXT NOT NULL,
		last_progress_at DATETIME,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create task table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE task_event (
		id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL,
		task_version INTEGER NOT NULL, event_type TEXT NOT NULL, from_status TEXT,
		to_status TEXT NOT NULL, actor_type TEXT NOT NULL, actor_ref TEXT,
		run_id INTEGER, detail TEXT, occurred_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		t.Fatalf("create task_event table: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO task(id, status, created_at, updated_at) VALUES (1, 'pending', '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z')`,
		`INSERT INTO task(id, status, last_progress_at, created_at, updated_at) VALUES (2, 'pending', '2026-08-01T12:00:00Z', '2026-07-02T00:00:00Z', '2026-08-01T12:00:00Z')`,
		`INSERT INTO task(id, status, created_at, updated_at) VALUES (3, 'done', '2026-08-02T00:00:00Z', '2026-08-02T00:00:00Z')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("insert task fixture: %v", err)
		}
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	list, err := store.ListTasks(t.Context(), TaskFilter{From: &from, Until: &until, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list.Items) != 2 || list.Total != 2 {
		t.Fatalf("time-window tasks = %#v total=%d", list.Items, list.Total)
	}
	pending, err := store.LoadPending(t.Context(), 20)
	if err != nil {
		t.Fatalf("LoadPending() error = %v", err)
	}
	if len(pending) != 2 || pending[0].ID != 1 || pending[1].ID != 2 {
		t.Fatalf("pending order = %#v", pending)
	}
}

func TestCloseResolvesTaskAndProjectsProactiveActor(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY, status TEXT NOT NULL, execution_result TEXT,
			summary TEXT, last_progress_at DATETIME, version INTEGER NOT NULL,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL,
			task_version INTEGER NOT NULL, event_type TEXT NOT NULL, from_status TEXT,
			to_status TEXT NOT NULL, actor_type TEXT NOT NULL, actor_ref TEXT,
			run_id INTEGER, detail TEXT, occurred_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(task_id, task_version)
		)`,
		`CREATE TABLE scheduled_task (
			id INTEGER PRIMARY KEY, subject_type TEXT, subject_id INTEGER,
			dispatch_kind TEXT, source_run_id INTEGER, status TEXT, last_run_status TEXT,
			last_error_detail TEXT, last_finished_at DATETIME, updated_at DATETIME
		)`,
		`INSERT INTO task(id, status, execution_result, version, created_at, updated_at)
		 VALUES (7, 'waiting', '{"stage":"proposal","summary":"原执行结论","proposal":{"action":"发消息"}}', 3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO scheduled_task(id, subject_type, subject_id, dispatch_kind, status)
		 VALUES (9, 'task', 7, 'resume_task', 'binding')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("fixture statement failed: %v", err)
		}
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	view, err := store.Close(t.Context(), CloseInput{
		TaskID: 7, ExpectedVersion: 3, ActorType: "proactive",
		Result: []byte(`{"stage":"proactive_closed","summary":"昨日任务已过期","evidence":"截止时间已过"}`),
	})
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if view.Status != "done" || view.Version != 4 || view.Resolution == nil || view.Resolution.ActorType != "proactive" {
		t.Fatalf("closed view = %#v", view)
	}
	if !strings.Contains(string(view.ExecutionResult), `"stage":"proposal"`) {
		t.Fatalf("close replaced execution_result: %s", view.ExecutionResult)
	}
	if view.Summary == nil || *view.Summary != "昨日任务已过期" {
		t.Fatalf("close summary = %#v", view.Summary)
	}
	loaded, err := store.GetTask(t.Context(), 7)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if loaded.Resolution == nil || loaded.Resolution.EventType != "closed" || loaded.Resolution.ActorType != "proactive" {
		t.Fatalf("loaded resolution = %#v", loaded.Resolution)
	}
	if !strings.Contains(string(loaded.ExecutionResult), `"stage":"proposal"`) {
		t.Fatalf("persisted execution_result was replaced: %s", loaded.ExecutionResult)
	}
	var closeDetail string
	if err := db.Raw("SELECT detail FROM task_event WHERE task_id = 7 AND event_type = 'closed'").Scan(&closeDetail).Error; err != nil {
		t.Fatalf("load close event detail: %v", err)
	}
	if !strings.Contains(closeDetail, `"stage":"proactive_closed"`) || !strings.Contains(closeDetail, `"evidence":"截止时间已过"`) {
		t.Fatalf("close event detail = %s", closeDetail)
	}
	var scheduleStatus string
	if err := db.Raw("SELECT status FROM scheduled_task WHERE id = 9").Scan(&scheduleStatus).Error; err != nil {
		t.Fatalf("load scheduled task: %v", err)
	}
	if scheduleStatus != "completed" {
		t.Fatalf("scheduled task status = %q, want completed", scheduleStatus)
	}
}

func TestUpdateTaskMaintainsMutableSurfaceAndFrozenEvidence(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY, title TEXT NOT NULL, action_type TEXT NOT NULL,
			target TEXT NOT NULL, background TEXT NOT NULL, source_payload TEXT NOT NULL,
			status TEXT NOT NULL, summary TEXT, last_progress_at DATETIME,
			execution_supplements TEXT, version INTEGER NOT NULL,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL,
			task_version INTEGER NOT NULL, event_type TEXT NOT NULL, from_status TEXT,
			to_status TEXT NOT NULL, actor_type TEXT NOT NULL, actor_ref TEXT,
			run_id INTEGER, detail TEXT, occurred_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(task_id, task_version)
		)`,
		`INSERT INTO task(id,title,action_type,target,background,source_payload,status,execution_supplements,version,created_at,updated_at)
		 VALUES (8,'旧标题','agent_task','旧目标','{"snapshot":"frozen"}','{"clue":"frozen"}','waiting','[]',2,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO task(id,title,action_type,target,background,source_payload,status,execution_supplements,version,created_at,updated_at)
		 VALUES (9,'待审批','agent_task','待审目标','{}','{}','awaiting_approval','[]',4,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("fixture statement failed: %v", err)
		}
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	title, target := "更新后的标题", "等待权限后完成文档入库"
	summary, instruction := "权限申请仍有效，等待 owner 审批", "恢复后先核验权限，再继续转换文档"
	view, err := store.UpdateTask(t.Context(), TaskUpdateInput{
		TaskID: 8, ExpectedVersion: 2, Title: &title, Target: &target,
		Summary: &summary, Instruction: &instruction,
		Reason: "跨日后等待条件仍有效", ActorType: "proactive",
	})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if view.Status != "waiting" || view.Version != 3 || view.Title != title || view.Target != target || view.Summary == nil || *view.Summary != summary {
		t.Fatalf("updated view = %#v", view)
	}
	if string(view.Background) != `{"snapshot":"frozen"}` || string(view.SourcePayload) != `{"clue":"frozen"}` {
		t.Fatalf("frozen evidence changed: background=%s source_payload=%s", view.Background, view.SourcePayload)
	}
	if len(view.ExecutionSupplements) != 1 || view.ExecutionSupplements[0].Note != instruction || view.ExecutionSupplements[0].Channel != "proactive_agent" {
		t.Fatalf("execution supplements = %#v", view.ExecutionSupplements)
	}
	var event domain.TaskEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.EventType != "updated" || event.ActorType != "proactive" || event.ToStatus != "waiting" || !strings.Contains(string(event.Detail), "跨日后等待条件仍有效") {
		t.Fatalf("update event = %#v detail=%s", event, event.Detail)
	}
	if _, err := store.UpdateTask(t.Context(), TaskUpdateInput{
		TaskID: 9, ExpectedVersion: 4, Instruction: &instruction,
		Reason: "不能暗改待审批方案", ActorType: "proactive",
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("awaiting_approval instruction update error = %v", err)
	}
}

func TestFailStaleExecutingRejectsInvalidInput(t *testing.T) {
	s := &Store{}
	if _, err := s.FailStaleExecuting(context.Background(), 0, time.Now()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("olderThan=0 error = %v", err)
	}
	if _, err := s.FailStaleExecuting(context.Background(), 30*time.Minute, time.Time{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero now error = %v", err)
	}
}

func newStaleSweepDB(t *testing.T) *gorm.DB {
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
			id INTEGER PRIMARY KEY,
			status TEXT NOT NULL,
			execution_result TEXT,
			version INTEGER NOT NULL,
			updated_at DATETIME
		)`,
		`CREATE TABLE execution_run (
			id INTEGER PRIMARY KEY,
			task_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			error_detail TEXT,
			finished_at DATETIME
		)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			task_version INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			from_status TEXT,
			to_status TEXT NOT NULL,
			actor_type TEXT NOT NULL,
			actor_ref TEXT,
			run_id INTEGER,
			detail TEXT,
			occurred_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(task_id, task_version)
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	return db
}

func TestFailStaleExecutingNormalizesSQLiteTimezoneOffsets(t *testing.T) {
	db := newStaleSweepDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := db.Exec(
		"INSERT INTO task(id, status, version, updated_at) VALUES (?, ?, ?, ?), (?, ?, ?, ?)",
		uint64(102), "executing", int32(4), "2026-08-06 14:52:29.208714+08:00",
		uint64(115), "executing", int32(1), "2026-08-06 15:25:04.079434+08:00",
	).Error; err != nil {
		t.Fatalf("insert Tasks: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO execution_run(id, task_id, status) VALUES (?, ?, ?)",
		uint64(122), uint64(102), "running",
	).Error; err != nil {
		t.Fatalf("insert ExecutionRun: %v", err)
	}

	now := time.Date(2026, 8, 6, 7, 45, 0, 0, time.UTC)
	sweep, err := store.FailStaleExecuting(context.Background(), 45*time.Minute, now)
	if err != nil {
		t.Fatalf("FailStaleExecuting() error = %v", err)
	}
	if sweep.Failed != 1 || sweep.Requeued != 0 {
		t.Fatalf("FailStaleExecuting() = %+v, want one failure", sweep)
	}
	var stale, fresh domain.Task
	if err := db.First(&stale, 102).Error; err != nil {
		t.Fatalf("load stale Task: %v", err)
	}
	if err := db.First(&fresh, 115).Error; err != nil {
		t.Fatalf("load fresh Task: %v", err)
	}
	if stale.Status != "failed" || stale.Version != 5 {
		t.Fatalf("stale Task = status %q version %d", stale.Status, stale.Version)
	}
	if fresh.Status != "executing" || fresh.Version != 1 {
		t.Fatalf("fresh Task = status %q version %d", fresh.Status, fresh.Version)
	}
	var run domain.ExecutionRun
	if err := db.First(&run, 122).Error; err != nil {
		t.Fatalf("load ExecutionRun: %v", err)
	}
	if run.Status != "failed" || run.FinishedAt == nil {
		t.Fatalf("ExecutionRun = status %q finished_at %v", run.Status, run.FinishedAt)
	}
	var event domain.TaskEvent
	if err := db.Where("task_id = ?", 102).First(&event).Error; err != nil {
		t.Fatalf("load TaskEvent: %v", err)
	}
	if event.EventType != "stale_failed" || event.ToStatus != "failed" {
		t.Fatalf("TaskEvent = %#v", event)
	}
}

// TestFailStaleExecutingRequeuesTasksWhoseAgentNeverStarted pins how the two
// kinds of zombie are told apart. A Task whose agent never got a run row cannot
// have written anything to the outside world, so losing it to a terminal
// failure — which is how two meeting write-ups were silently dropped on
// 2026-08-06 — is pure waste; it goes back in the queue. A Task that did start
// may already have sent a message, so it still fails and waits for a human.
func TestFailStaleExecutingRequeuesTasksWhoseAgentNeverStarted(t *testing.T) {
	db := newStaleSweepDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := db.Exec(
		"INSERT INTO task(id, status, version, updated_at) VALUES (?, ?, ?, ?), (?, ?, ?, ?)",
		uint64(110), "executing", int32(1), "2026-08-06 06:55:16+00:00",
		uint64(106), "executing", int32(2), "2026-08-06 06:55:16+00:00",
	).Error; err != nil {
		t.Fatalf("insert Tasks: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO execution_run(id, task_id, status) VALUES (?, ?, ?)",
		uint64(128), uint64(106), "running",
	).Error; err != nil {
		t.Fatalf("insert ExecutionRun: %v", err)
	}

	now := time.Date(2026, 8, 6, 7, 51, 51, 0, time.UTC)
	sweep, err := store.FailStaleExecuting(context.Background(), 45*time.Minute, now)
	if err != nil {
		t.Fatalf("FailStaleExecuting() error = %v", err)
	}
	if sweep.Requeued != 1 || sweep.Failed != 1 {
		t.Fatalf("FailStaleExecuting() = %+v, want one requeue and one failure", sweep)
	}
	var neverStarted, started domain.Task
	if err := db.First(&neverStarted, 110).Error; err != nil {
		t.Fatalf("load requeued Task: %v", err)
	}
	if err := db.First(&started, 106).Error; err != nil {
		t.Fatalf("load failed Task: %v", err)
	}
	if neverStarted.Status != "pending" || neverStarted.Version != 2 {
		t.Fatalf("Task without a run = status %q version %d, want pending", neverStarted.Status, neverStarted.Version)
	}
	if started.Status != "failed" {
		t.Fatalf("Task with a run = status %q, want failed", started.Status)
	}
	var event domain.TaskEvent
	if err := db.Where("task_id = ?", 110).First(&event).Error; err != nil {
		t.Fatalf("load TaskEvent: %v", err)
	}
	if event.EventType != "stale_requeued" || event.ToStatus != "pending" {
		t.Fatalf("TaskEvent = %#v", event)
	}
}

// TestRecordProgressMovesTimestampOnlyOnChange pins the reason last_progress_at
// exists: a Task that keeps resuming and re-reporting the same standing must not
// look alive, or the field cannot be used to find stalled work.
func TestRecordProgressMovesTimestampOnlyOnChange(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE task (
		id INTEGER PRIMARY KEY,
		status TEXT NOT NULL,
		summary TEXT,
		last_progress_at DATETIME,
		version INTEGER NOT NULL,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create test table: %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	const taskID = uint64(7)
	if err := db.Exec("INSERT INTO task(id, status, version) VALUES (?, ?, ?)", taskID, "executing", 3).Error; err != nil {
		t.Fatalf("create Task: %v", err)
	}
	load := func() domain.Task {
		var task domain.Task
		if err := db.First(&task, taskID).Error; err != nil {
			t.Fatalf("load Task: %v", err)
		}
		return task
	}

	first := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	if err := store.RecordProgress(context.Background(), taskID, "  等对方回权限  ", first); err != nil {
		t.Fatalf("RecordProgress() error = %v", err)
	}
	task := load()
	if task.Summary == nil || *task.Summary != "等对方回权限" {
		t.Fatalf("summary = %v, want trimmed text", task.Summary)
	}
	if task.LastProgressAt == nil || !task.LastProgressAt.Equal(first) {
		t.Fatalf("last_progress_at = %v, want %v", task.LastProgressAt, first)
	}
	if task.Version != 3 {
		t.Fatalf("version = %d, want it untouched: progress must not consume the optimistic lock", task.Version)
	}

	// Same standing re-reported (whitespace aside): a resume that moved nothing.
	if err := store.RecordProgress(context.Background(), taskID, "等对方回权限\n", first.Add(time.Hour)); err != nil {
		t.Fatalf("RecordProgress() unchanged error = %v", err)
	}
	if got := load(); got.LastProgressAt == nil || !got.LastProgressAt.Equal(first) {
		t.Fatalf("last_progress_at = %v, want it to stay at %v for an unchanged summary", got.LastProgressAt, first)
	}

	// Blank means "this run moved nothing"; it must not erase what is stored.
	if err := store.RecordProgress(context.Background(), taskID, "   ", first.Add(2*time.Hour)); err != nil {
		t.Fatalf("RecordProgress() blank error = %v", err)
	}
	task = load()
	if task.Summary == nil || *task.Summary != "等对方回权限" {
		t.Fatalf("summary = %v, want a blank summary to be a no-op, not an erasure", task.Summary)
	}
	if task.LastProgressAt == nil || !task.LastProgressAt.Equal(first) {
		t.Fatalf("last_progress_at = %v, want it unchanged by a blank summary", task.LastProgressAt)
	}

	third := first.Add(3 * time.Hour)
	if err := store.RecordProgress(context.Background(), taskID, "权限已下来，开始改代码", third); err != nil {
		t.Fatalf("RecordProgress() changed error = %v", err)
	}
	if got := load(); got.LastProgressAt == nil || !got.LastProgressAt.Equal(third) {
		t.Fatalf("last_progress_at = %v, want %v after real movement", got.LastProgressAt, third)
	}
}
