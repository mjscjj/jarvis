package effectops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// fakeRecallClient records the recall calls a test triggered.
type fakeRecallClient struct {
	calls []string
	err   error
}

func (f *fakeRecallClient) RecallMessage(_ context.Context, messageID string) error {
	f.calls = append(f.calls, messageID)
	return f.err
}

func newRecallTestDB(t *testing.T) *gorm.DB {
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
			execution_supplements TEXT,
			version INTEGER NOT NULL,
			updated_at DATETIME
		)`,
		`CREATE TABLE execution_run (
			id INTEGER PRIMARY KEY,
			task_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			effects TEXT
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

func newRecaller(t *testing.T, db *gorm.DB, lark MessageRecallClient) *MessageRecaller {
	t.Helper()
	recaller, err := NewMessageRecaller(db, lark)
	if err != nil {
		t.Fatalf("NewMessageRecaller() error = %v", err)
	}
	recaller.now = func() time.Time { return time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC) }
	return recaller
}

// seedRecallTask stores one done Task whose execution_result and whose run both
// declare the same sent message — the real shape produced by M5.
func seedRecallTask(t *testing.T, db *gorm.DB, executionResult, runEffects string) {
	t.Helper()
	if err := db.Exec(
		"INSERT INTO task(id, status, execution_result, version) VALUES (?, ?, ?, ?)",
		1, "done", executionResult, 3,
	).Error; err != nil {
		t.Fatalf("create Task: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO execution_run(id, task_id, status, effects) VALUES (?, ?, ?, ?)",
		9, 1, "succeeded", runEffects,
	).Error; err != nil {
		t.Fatalf("create execution run: %v", err)
	}
}

func TestRecallMarksBothCopiesAndAppendsEvent(t *testing.T) {
	db := newRecallTestDB(t)
	seedRecallTask(t, db,
		`{"stage":"executed","summary":"已在群里同步","source_run_id":9,"effects":[{"kind":"feishu_message","title":"进度同步","target":"研发群","message_id":"om_a"},{"kind":"feishu_doc","title":"周报","doc_token":"doc_1"}]}`,
		`[{"kind":"feishu_message","title":"进度同步","target":"研发群","message_id":"om_a"}]`,
	)
	lark := &fakeRecallClient{}
	recaller := newRecaller(t, db, lark)

	err := recaller.Recall(t.Context(), 1, "om_a")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(lark.calls) != 1 || lark.calls[0] != "om_a" {
		t.Fatalf("lark recall calls = %v", lark.calls)
	}
	var task domain.Task
	if err := db.First(&task, 1).Error; err != nil {
		t.Fatalf("load Task: %v", err)
	}
	var result struct {
		Summary string                   `json:"summary"`
		Effects []map[string]interface{} `json:"effects"`
	}
	if err := json.Unmarshal(task.ExecutionResult, &result); err != nil {
		t.Fatalf("decode execution_result: %v", err)
	}
	if result.Summary != "已在群里同步" {
		t.Fatalf("execution_result lost sibling fields: %s", task.ExecutionResult)
	}
	if len(result.Effects) != 2 {
		t.Fatalf("effects = %#v", result.Effects)
	}
	if result.Effects[0]["recalled_at"] != "2026-07-30T08:00:00Z" {
		t.Fatalf("recalled effect = %#v", result.Effects[0])
	}
	if result.Effects[0]["title"] != "进度同步" || result.Effects[0]["message_id"] != "om_a" {
		t.Fatalf("declared fields rewritten: %#v", result.Effects[0])
	}
	if _, marked := result.Effects[1]["recalled_at"]; marked {
		t.Fatalf("unrelated effect marked: %#v", result.Effects[1])
	}

	var run domain.ExecutionRun
	if err := db.First(&run, 9).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if !strings.Contains(string(run.Effects), "2026-07-30T08:00:00Z") {
		t.Fatalf("run effects not marked: %s", run.Effects)
	}

	var events []domain.TaskEvent
	if err := db.Where("task_id = ?", 1).Find(&events).Error; err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "feishu_message_recalled" || events[0].TaskVersion != 4 {
		t.Fatalf("events = %#v", events)
	}
	if !strings.Contains(string(events[0].Detail), "om_a") {
		t.Fatalf("event detail = %s", events[0].Detail)
	}
}

// A message that was only declared by an older run still gets marked, and the
// Task version still moves so the audit event has a free version slot.
func TestRecallMarksRunOnlyDeclaration(t *testing.T) {
	db := newRecallTestDB(t)
	seedRecallTask(t, db,
		`{"stage":"executed","summary":"重跑后的结果","effects":[{"kind":"feishu_doc","doc_token":"doc_1"}]}`,
		`[{"kind":"feishu_message","message_id":"om_old"}]`,
	)
	lark := &fakeRecallClient{}
	recaller := newRecaller(t, db, lark)

	if err := recaller.Recall(t.Context(), 1, "om_old"); err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	var run domain.ExecutionRun
	if err := db.First(&run, 9).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if !strings.Contains(string(run.Effects), "recalled_at") {
		t.Fatalf("run effects not marked: %s", run.Effects)
	}
	var task domain.Task
	if err := db.First(&task, 1).Error; err != nil {
		t.Fatalf("load Task: %v", err)
	}
	if task.Version != 4 || strings.Contains(string(task.ExecutionResult), "recalled_at") {
		t.Fatalf("task = version %d result %s", task.Version, task.ExecutionResult)
	}
}

func TestRecallRejectsMessageThisTaskNeverSent(t *testing.T) {
	db := newRecallTestDB(t)
	seedRecallTask(t, db,
		`{"stage":"executed","effects":[{"kind":"feishu_message","message_id":"om_a"}]}`,
		`[{"kind":"feishu_message","message_id":"om_a"}]`,
	)
	lark := &fakeRecallClient{}
	recaller := newRecaller(t, db, lark)

	err := recaller.Recall(t.Context(), 1, "om_somebody_else")
	if !errors.Is(err, ErrRecallTargetNotFound) {
		t.Fatalf("Recall() error = %v, want ErrRecallTargetNotFound", err)
	}
	if len(lark.calls) != 0 {
		t.Fatalf("lark called for a message this Task never sent: %v", lark.calls)
	}
}

func TestRecallRejectsSecondRecall(t *testing.T) {
	db := newRecallTestDB(t)
	seedRecallTask(t, db,
		`{"stage":"executed","effects":[{"kind":"feishu_message","message_id":"om_a","recalled_at":"2026-07-29T10:00:00Z"}]}`,
		`[{"kind":"feishu_message","message_id":"om_a","recalled_at":"2026-07-29T10:00:00Z"}]`,
	)
	lark := &fakeRecallClient{}
	recaller := newRecaller(t, db, lark)

	err := recaller.Recall(t.Context(), 1, "om_a")
	if !errors.Is(err, ErrMessageAlreadyRecalled) {
		t.Fatalf("Recall() error = %v, want ErrMessageAlreadyRecalled", err)
	}
	if len(lark.calls) != 0 {
		t.Fatalf("lark called for an already recalled message: %v", lark.calls)
	}
}

// A failing lark-cli recall must leave the stored effects untouched: nothing was
// recalled, so nothing may claim it was.
func TestRecallKeepsEffectsWhenLarkFails(t *testing.T) {
	db := newRecallTestDB(t)
	seedRecallTask(t, db,
		`{"stage":"executed","effects":[{"kind":"feishu_message","message_id":"om_a"}]}`,
		`[{"kind":"feishu_message","message_id":"om_a"}]`,
	)
	lark := &fakeRecallClient{err: errors.New("lark-cli api error: message not found")}
	recaller := newRecaller(t, db, lark)

	if err := recaller.Recall(t.Context(), 1, "om_a"); err == nil {
		t.Fatal("Recall() succeeded while lark-cli failed")
	}
	var task domain.Task
	if err := db.First(&task, 1).Error; err != nil {
		t.Fatalf("load Task: %v", err)
	}
	if task.Version != 3 || strings.Contains(string(task.ExecutionResult), "recalled_at") {
		t.Fatalf("task = version %d result %s", task.Version, task.ExecutionResult)
	}
	var events int64
	if err := db.Model(&domain.TaskEvent{}).Where("task_id = ?", 1).Count(&events).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 0 {
		t.Fatalf("events = %d, want 0", events)
	}
}

func TestRecallRejectsInvalidInput(t *testing.T) {
	recaller := &MessageRecaller{}
	if err := recaller.Recall(context.Background(), 0, "om_a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("task_id=0 error = %v", err)
	}
	for _, messageID := range []string{"", "  ", "cli_123", "om"} {
		if err := recaller.Recall(context.Background(), 1, messageID); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("message_id=%q error = %v", messageID, err)
		}
	}
}

func TestNewMessageRecallerRejectsMissingDependencies(t *testing.T) {
	if _, err := NewMessageRecaller(nil, &fakeRecallClient{}); err == nil {
		t.Fatal("NewMessageRecaller(nil db) succeeded")
	}
	if _, err := NewMessageRecaller(&gorm.DB{}, nil); err == nil {
		t.Fatal("NewMessageRecaller(nil lark) succeeded")
	}
}
