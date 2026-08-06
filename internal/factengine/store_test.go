package factengine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func minutes(base time.Time, offset int) int64 {
	return base.Add(time.Duration(offset) * time.Minute).UnixMilli()
}

func TestSplitWindowsCutsOnGapAndSize(t *testing.T) {
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	rows := []messageRow{
		{ID: 1, CreateTime: minutes(base, 0)},
		{ID: 2, CreateTime: minutes(base, 5)},
		// 90 minutes later: a different conversation, not a continuation.
		{ID: 3, CreateTime: minutes(base, 95)},
		{ID: 4, CreateTime: minutes(base, 96)},
		{ID: 5, CreateTime: minutes(base, 97)},
	}
	windows := splitWindows(rows, 30*time.Minute, 40)
	if len(windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(windows))
	}
	if len(windows[0]) != 2 || len(windows[1]) != 3 {
		t.Fatalf("window sizes = %d/%d, want 2/3", len(windows[0]), len(windows[1]))
	}

	// A conversation with no pauses still gets cut, so one busy chat cannot
	// produce a single unbounded prompt.
	sized := splitWindows(rows, 30*time.Minute, 2)
	if len(sized) != 3 {
		t.Fatalf("size-capped windows = %d, want 3", len(sized))
	}
}

func TestGroupByChatOrdersEachChatByConversationTime(t *testing.T) {
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	// Insert order (id) and conversation order (create_time) disagree: a backfill
	// stored an older message later. Windowing has to see the conversation.
	rows := []messageRow{
		{ID: 1, ChatID: "a", CreateTime: minutes(base, 10)},
		{ID: 2, ChatID: "b", CreateTime: minutes(base, 0)},
		{ID: 3, ChatID: "a", CreateTime: minutes(base, 5)},
	}
	groups := groupByChat(rows)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if groups[0][0].ID != 3 || groups[0][1].ID != 1 {
		t.Fatalf("chat a order = %d,%d, want 3,1", groups[0][0].ID, groups[0][1].ID)
	}
}

func TestBuildUnitKeepsEveryCapturedMessage(t *testing.T) {
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	rows := []messageRow{
		{ID: 1, MessageID: "om_user", ChatID: "chat-a", ChatName: "上下文群", ChatMode: "group", GroupID: 3, Content: "方案定了走 B", SenderType: "user", SenderName: "张三", CreateTime: minutes(base, 0), RenderOK: true},
		{ID: 2, MessageID: "om_bot", ChatID: "chat-a", ChatName: "上下文群", ChatMode: "group", GroupID: 3, Content: "构建成功", SenderType: "bot", SenderName: "机器人", CreateTime: minutes(base, 1), RenderOK: true},
		{ID: 3, MessageID: "om_raw", ChatID: "chat-a", ChatName: "上下文群", ChatMode: "group", GroupID: 3, Content: "看不懂的原始内容", SenderType: "user", SenderName: "李四", CreateTime: minutes(base, 2), RenderOK: false},
	}
	unit := buildUnit(rows, nil, time.UTC)
	for _, want := range []string{"om_user", "om_bot", "om_raw", "sender_type=bot", "render_ok=false"} {
		if !strings.Contains(unit.Body, want) {
			t.Fatalf("body missing %q:\n%s", want, unit.Body)
		}
	}
}

func TestBuildUnitOffersProjectGroupAndPersonSubjects(t *testing.T) {
	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	projectID := uint64(7)
	projectName := "Jarvis"
	rows := []messageRow{
		{ID: 10, MessageID: "om_10", ChatID: "chat-a", ChatName: "研发群", ChatMode: "group", GroupID: 3, ProjectID: &projectID,
			ProjectName: &projectName, SenderOpenID: "ou_1", SenderName: "张三",
			Content: "方案定了走 B", CreateTime: minutes(base, 0), RenderOK: true},
		{ID: 11, MessageID: "om_11", ChatID: "chat-a", ChatName: "研发群", ChatMode: "group", GroupID: 3, ProjectID: &projectID,
			ProjectName: &projectName, SenderOpenID: "ou_2", SenderName: "李四",
			Content: "我明天上线", CreateTime: minutes(base, 2), RenderOK: true},
		// Same person speaking twice must not appear twice in the subject list.
		{ID: 12, MessageID: "om_12", ChatID: "chat-a", ChatName: "研发群", ChatMode: "group", GroupID: 3, ProjectID: &projectID,
			ProjectName: &projectName, SenderOpenID: "ou_1", SenderName: "张三",
			Content: "好", CreateTime: minutes(base, 3), RenderOK: true},
	}
	persons := map[string]Subject{
		"ou_1": {Type: "person", ID: 21, Name: "张三"},
		// ou_2 is not a tracked person: no subject, but the words still get read.
	}
	unit := buildUnit(rows, persons, time.UTC)

	if unit.Source != SourceMessage || unit.LastID != 12 {
		t.Fatalf("unit source/last_id = %s/%d", unit.Source, unit.LastID)
	}
	if !unit.OccurredAt.Equal(time.UnixMilli(minutes(base, 3))) {
		t.Fatalf("unit occurred_at = %v", unit.OccurredAt)
	}
	want := []Subject{
		{Type: "group", ID: 3, Name: "研发群"},
		{Type: "person", ID: 21, Name: "张三"},
		{Type: "project", ID: 7, Name: "Jarvis"},
	}
	if len(unit.Subjects) != len(want) {
		t.Fatalf("subjects = %+v, want %+v", unit.Subjects, want)
	}
	for i := range want {
		if unit.Subjects[i] != want[i] {
			t.Fatalf("subject %d = %+v, want %+v", i, unit.Subjects[i], want[i])
		}
	}
	for _, fragment := range []string{"sender_name=\"张三\"", "方案定了走 B", "我明天上线", "2026-08-01T09:00:00Z", "known_association: project/7"} {
		text := unit.Body + "\n" + unit.Context
		if !strings.Contains(text, fragment) {
			t.Fatalf("unit missing %q:\n%s", fragment, text)
		}
	}
}

// An unbound chat still offers its group, so a fact from it has somewhere to go.
func TestBuildUnitWithoutProjectStillOffersGroup(t *testing.T) {
	rows := []messageRow{{ID: 1, ChatID: "chat-b", ChatName: "临时群", GroupID: 9,
		SenderOpenID: "ou_x", SenderName: "王五", Content: "记一下", CreateTime: time.Now().UnixMilli(), RenderOK: true}}
	unit := buildUnit(rows, map[string]Subject{}, time.UTC)
	if len(unit.Subjects) != 1 || unit.Subjects[0].Type != "group" || unit.Subjects[0].ID != 9 {
		t.Fatalf("subjects = %+v, want only group 9", unit.Subjects)
	}
}

func TestWindowOptionsValidate(t *testing.T) {
	valid := WindowOptions{Gap: time.Minute, MaxMessages: 10, Location: time.UTC}
	if err := valid.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	for _, tt := range []struct {
		name string
		opts WindowOptions
	}{
		{"no gap", WindowOptions{MaxMessages: 10, Location: time.UTC}},
		{"no max", WindowOptions{Gap: time.Minute, Location: time.UTC}},
		{"no location", WindowOptions{Gap: time.Minute, MaxMessages: 10}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.opts.validate(); err == nil {
				t.Fatal("validate() error = nil, want rejection")
			}
		})
	}
}

func TestAdvanceCursorUsesSQLiteUpsertAndNeverMovesBackward(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.FactSourceCursor{}); err != nil {
		t.Fatalf("migrate fact source cursor: %v", err)
	}
	store, err := NewGORMStore(db)
	if err != nil {
		t.Fatalf("NewGORMStore: %v", err)
	}
	ctx := context.Background()
	if err := store.AdvanceCursor(ctx, SourceTask, 10, time.Time{}); err != nil {
		t.Fatalf("insert cursor: %v", err)
	}
	staleUpdatedAt := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Model(&domain.FactSourceCursor{}).Where("source = ?", SourceTask).Update("updated_at", staleUpdatedAt).Error; err != nil {
		t.Fatalf("make cursor timestamp stale: %v", err)
	}
	if err := store.AdvanceCursor(ctx, SourceTask, 8, time.Time{}); err != nil {
		t.Fatalf("upsert stale cursor: %v", err)
	}
	lastID, found, err := store.Cursor(ctx, SourceTask)
	if err != nil {
		t.Fatalf("load cursor: %v", err)
	}
	if !found || lastID != 10 {
		t.Fatalf("cursor found/last_id = %v/%d, want true/10", found, lastID)
	}
	var row domain.FactSourceCursor
	if err := db.Where("source = ?", SourceTask).First(&row).Error; err != nil {
		t.Fatalf("load cursor row: %v", err)
	}
	if !row.UpdatedAt.After(staleUpdatedAt) {
		t.Fatalf("cursor updated_at = %s, want newer than %s", row.UpdatedAt, staleUpdatedAt)
	}
}

func TestTodoAndTaskUnitsPassLifecycleEventAndCurrentRow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE todo (id INTEGER PRIMARY KEY, title TEXT, description TEXT, action_type TEXT, target TEXT, context TEXT, open_questions JSON, commitment_strength TEXT, source_message_ids JSON, source_quote TEXT, group_id INTEGER, project_id INTEGER, status TEXT, dedup_fingerprint TEXT, revision INTEGER, first_seen_at DATETIME, last_evidence_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE todo_event (id INTEGER PRIMARY KEY, todo_id INTEGER, from_status TEXT, to_status TEXT, actor TEXT, detail JSON, snapshot JSON, created_at DATETIME)`,
		`CREATE TABLE task (id INTEGER PRIMARY KEY, todo_id INTEGER, title TEXT, action_type TEXT, target TEXT, background JSON, source_payload JSON, source_type TEXT, status TEXT, execution_result JSON, project_id INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE execution_run (id INTEGER PRIMARY KEY, task_id INTEGER, action_type TEXT, stage TEXT, sandbox TEXT, status TEXT, prompt TEXT, summary TEXT, output JSON, effects JSON, started_at DATETIME, finished_at DATETIME, created_at DATETIME)`,
		`CREATE TABLE task_event (id INTEGER PRIMARY KEY, task_id INTEGER, task_version INTEGER, event_type TEXT, from_status TEXT, to_status TEXT, actor_type TEXT, actor_ref TEXT, run_id INTEGER, detail JSON, occurred_at DATETIME, created_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create material table: %v", err)
		}
	}
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	groupID, projectID := uint64(3), uint64(7)
	todo := domain.Todo{
		ID: 11, Title: "接入通用事实源", Description: "把材料直接交给 Agent", ActionType: "agent_task",
		Target: "factengine", Context: "完整背景", OpenQuestions: []byte(`[]`), CommitmentStrength: "firm",
		SourceMessageIDs: []byte(`["om_1"]`), SourceQuote: "全都扔进去", GroupID: &groupID, ProjectID: &projectID,
		Status: "extracted", DedupFingerprint: strings.Repeat("a", 64), Revision: 1,
		FirstSeenAt: now, LastEvidenceAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Table("todo").Create(map[string]any{
		"id": todo.ID, "title": todo.Title, "description": todo.Description, "action_type": todo.ActionType,
		"target": todo.Target, "context": todo.Context, "open_questions": todo.OpenQuestions,
		"commitment_strength": todo.CommitmentStrength, "source_message_ids": todo.SourceMessageIDs,
		"source_quote": todo.SourceQuote, "group_id": todo.GroupID, "project_id": todo.ProjectID,
		"status": todo.Status, "dedup_fingerprint": todo.DedupFingerprint, "revision": todo.Revision,
		"first_seen_at": todo.FirstSeenAt, "last_evidence_at": todo.LastEvidenceAt,
		"created_at": todo.CreatedAt, "updated_at": todo.UpdatedAt,
	}).Error; err != nil {
		t.Fatalf("create todo: %v", err)
	}
	todoEvent := domain.TodoEvent{ID: 21, TodoID: todo.ID, ToStatus: "extracted", Actor: "m3", Detail: []byte(`{"kind":"created"}`), Snapshot: []byte(`{"title":"接入通用事实源"}`), CreatedAt: now}
	if err := db.Table("todo_event").Create(map[string]any{
		"id": todoEvent.ID, "todo_id": todoEvent.TodoID, "to_status": todoEvent.ToStatus,
		"actor": todoEvent.Actor, "detail": todoEvent.Detail, "snapshot": todoEvent.Snapshot, "created_at": todoEvent.CreatedAt,
	}).Error; err != nil {
		t.Fatalf("create todo event: %v", err)
	}
	task := domain.Task{
		ID: 31, TodoID: &todo.ID, Title: "实现通用事实源", ActionType: "agent_task", Target: "factengine",
		Background: []byte(`{"context":"完整背景"}`), SourcePayload: []byte(`{"title":"接入通用事实源","steps":["实现"]}`),
		SourceType: "todo",
		Status:     "succeeded", ExecutionResult: []byte(`{"summary":"已经完成"}`), ProjectID: &projectID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Table("task").Create(map[string]any{
		"id": task.ID, "todo_id": task.TodoID, "title": task.Title, "action_type": task.ActionType,
		"target": task.Target, "background": task.Background, "source_payload": task.SourcePayload,
		"source_type": task.SourceType,
		"status":      task.Status, "execution_result": task.ExecutionResult, "project_id": task.ProjectID,
		"created_at": task.CreatedAt, "updated_at": task.UpdatedAt,
	}).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	summary := "实现并验证完成"
	run := domain.ExecutionRun{ID: 41, TaskID: task.ID, ActionType: "agent_task", Stage: "execute", Sandbox: "danger-full-access", Status: "succeeded", Prompt: "执行任务", Summary: &summary, Output: []byte(`{"ok":true}`), StartedAt: now, FinishedAt: &now, CreatedAt: now}
	if err := db.Table("execution_run").Create(map[string]any{
		"id": run.ID, "task_id": run.TaskID, "action_type": run.ActionType, "stage": run.Stage,
		"sandbox": run.Sandbox, "status": run.Status, "prompt": run.Prompt, "summary": run.Summary,
		"output": run.Output, "started_at": run.StartedAt, "finished_at": run.FinishedAt, "created_at": run.CreatedAt,
	}).Error; err != nil {
		t.Fatalf("create execution run: %v", err)
	}
	taskEvent := domain.TaskEvent{ID: 51, TaskID: task.ID, TaskVersion: 1, EventType: "executed", ToStatus: "succeeded", ActorType: "agent", RunID: &run.ID, Detail: []byte(`{"summary":"实现并验证完成"}`), OccurredAt: now, CreatedAt: now}
	if err := db.Table("task_event").Create(map[string]any{
		"id": taskEvent.ID, "task_id": taskEvent.TaskID, "task_version": taskEvent.TaskVersion,
		"event_type": taskEvent.EventType, "to_status": taskEvent.ToStatus, "actor_type": taskEvent.ActorType,
		"run_id": taskEvent.RunID, "detail": taskEvent.Detail, "occurred_at": taskEvent.OccurredAt, "created_at": taskEvent.CreatedAt,
	}).Error; err != nil {
		t.Fatalf("create task event: %v", err)
	}

	store, err := NewGORMStore(db)
	if err != nil {
		t.Fatalf("NewGORMStore: %v", err)
	}
	opts := WindowOptions{Gap: 30 * time.Minute, MaxMessages: 40, Location: time.UTC}
	todoUnits, todoMax, err := store.TodoUnits(context.Background(), 0, 10, opts)
	if err != nil {
		t.Fatalf("TodoUnits: %v", err)
	}
	if todoMax != 21 || len(todoUnits) != 1 || todoUnits[0].Source != SourceTodo {
		t.Fatalf("todo units=%+v max=%d", todoUnits, todoMax)
	}
	for _, fragment := range []string{`"event"`, `"current_todo"`, "接入通用事实源", "全都扔进去"} {
		if !strings.Contains(todoUnits[0].Body, fragment) {
			t.Fatalf("todo material missing %q:\n%s", fragment, todoUnits[0].Body)
		}
	}

	taskUnits, taskMax, err := store.TaskUnits(context.Background(), 0, 10, opts)
	if err != nil {
		t.Fatalf("TaskUnits: %v", err)
	}
	if taskMax != 51 || len(taskUnits) != 1 || taskUnits[0].Source != SourceTask {
		t.Fatalf("task units=%+v max=%d", taskUnits, taskMax)
	}
	for _, fragment := range []string{`"event"`, `"current_task"`, `"execution_run"`, "实现并验证完成"} {
		if !strings.Contains(taskUnits[0].Body, fragment) {
			t.Fatalf("task material missing %q:\n%s", fragment, taskUnits[0].Body)
		}
	}
}

func TestMaterialWindowEndCutsOnNaturalDayAndSize(t *testing.T) {
	times := []time.Time{
		time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC),
	}
	at := func(i int) time.Time { return times[i] }
	if end := materialWindowEnd(0, len(times), 2, time.UTC, at); end != 2 {
		t.Fatalf("size cut end=%d, want 2", end)
	}
	if end := materialWindowEnd(2, len(times), 10, time.UTC, at); end != 3 {
		t.Fatalf("day cut end=%d, want 3", end)
	}
}
