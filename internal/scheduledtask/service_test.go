package scheduledtask

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"
	"jarvis/internal/taskcreate"
)

type okrProgressSubmitter struct {
	inputs []taskcreate.Input
}

func (s *okrProgressSubmitter) Submit(_ context.Context, input taskcreate.Input) (*domain.Task, error) {
	s.inputs = append(s.inputs, input)
	return &domain.Task{ID: 77}, nil
}

type okrProgressResumer struct{}

func (okrProgressResumer) ResumeTask(context.Context, uint64, uint64, string) error { return nil }

func TestNormalizeInput(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	interval := 10
	input, nextRunAt, err := normalizeInput(Input{
		Title: "  周五跟进  ", Instruction: "  查询最新进展  ",
		ContextSnapshot: json.RawMessage(`{"project":{"id":45}}`),
		ScheduleType:    "interval", IntervalMinutes: &interval,
	}, now, now.Location())
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.Title != "周五跟进" || input.Instruction != "查询最新进展" {
		t.Fatalf("normalized text = %#v", input)
	}
	if string(input.ContextSnapshot) != `{"project":{"id":45}}` {
		t.Fatalf("context = %s", input.ContextSnapshot)
	}
	if !*input.Enabled || !nextRunAt.Equal(now.Add(10*time.Minute).UTC()) {
		t.Fatalf("enabled=%t next_run_at=%s", *input.Enabled, nextRunAt)
	}
}

func TestNormalizeInputRejectsInvalidContext(t *testing.T) {
	t.Parallel()
	now := time.Now()
	interval := 10
	for _, raw := range []string{`[]`, `null`, `{"a":1} {"b":2}`, `{broken`} {
		_, _, err := normalizeInput(Input{
			Title: "x", Instruction: "y", ContextSnapshot: json.RawMessage(raw),
			ScheduleType: "interval", IntervalMinutes: &interval,
		}, now, time.Local)
		if err == nil {
			t.Fatalf("normalizeInput() accepted context %q", raw)
		}
	}
}

func TestDailyAndIntervalNextOccurrence(t *testing.T) {
	t.Parallel()
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 7, 24, 9, 30, 0, 0, location)
	dailyTime := "09:00"
	_, dailyNext, err := normalizeInput(Input{
		Title: "daily", Instruction: "run", ScheduleType: "daily", DailyTime: &dailyTime,
	}, now, location)
	if err != nil {
		t.Fatalf("normalize daily input: %v", err)
	}
	wantDaily := time.Date(2026, 7, 25, 9, 0, 0, 0, location).UTC()
	if !dailyNext.Equal(wantDaily) {
		t.Fatalf("daily next = %s, want %s", dailyNext, wantDaily)
	}

	interval := 10
	oldNext := now.Add(-35 * time.Minute).UTC()
	intervalNext, err := nextOccurrence(&domain.ScheduledTask{
		ScheduleType: "interval", IntervalMinutes: &interval, NextRunAt: oldNext,
	}, now.UTC(), location)
	if err != nil {
		t.Fatalf("next interval occurrence: %v", err)
	}
	if want := now.Add(5 * time.Minute).UTC(); !intervalNext.Equal(want) {
		t.Fatalf("interval next = %s, want %s", intervalNext, want)
	}
}

func TestOneTimeNextOccurrenceAndFinalStatus(t *testing.T) {
	t.Parallel()
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 7, 24, 9, 30, 0, 0, location)
	runAt := now.Add(2 * time.Hour)
	input, nextRunAt, err := normalizeInput(Input{
		Title: "once", Instruction: "run", ScheduleType: "once", RunAt: &runAt,
	}, now, location)
	if err != nil {
		t.Fatalf("normalize once input: %v", err)
	}
	if input.DailyTime != nil || input.IntervalMinutes != nil || input.RunAt == nil {
		t.Fatalf("normalized once fields = %#v", input)
	}
	if !nextRunAt.Equal(runAt.UTC()) || !input.RunAt.Equal(runAt.UTC()) {
		t.Fatalf("once run_at=%v next_run_at=%v want=%v", input.RunAt, nextRunAt, runAt.UTC())
	}
	if got := finalTaskStatus("once"); got != "completed" {
		t.Fatalf("once final status = %q", got)
	}
	if got := finalTaskStatus("daily"); got != "active" {
		t.Fatalf("daily final status = %q", got)
	}
}

func TestNormalizeInputRejectsInvalidSchedule(t *testing.T) {
	t.Parallel()
	now := time.Now()
	badTime := "9:00"
	zero := 0
	for _, input := range []Input{
		{Title: "x", Instruction: "y", ScheduleType: "once"},
		{Title: "x", Instruction: "y", ScheduleType: "daily"},
		{Title: "x", Instruction: "y", ScheduleType: "daily", DailyTime: &badTime},
		{Title: "x", Instruction: "y", ScheduleType: "interval", IntervalMinutes: &zero},
		{Title: "x", Instruction: "y", ScheduleType: "cron"},
	} {
		if _, _, err := normalizeInput(input, now, time.Local); err == nil {
			t.Fatalf("normalizeInput() accepted %#v", input)
		}
	}
}

func TestNormalizeInputDefaultsAgentTaskActionType(t *testing.T) {
	t.Parallel()
	runAt := time.Now().Add(time.Hour)
	input, _, err := normalizeInput(Input{
		Title: "未来任务", Instruction: "明早查询 Agent Runtime 最新状态",
		ContextSnapshot: json.RawMessage(`{"project":{"name":"Agent Runtime"},"chat_id":"oc_x"}`),
		ScheduleType:    "once", RunAt: &runAt,
	}, time.Now(), time.Local)
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.ActionType != "agent_task" {
		t.Fatalf("action_type = %q, want agent_task", input.ActionType)
	}
}

func TestNormalizeResumeTaskOnlyAcceptsYieldBinding(t *testing.T) {
	t.Parallel()
	runAt := time.Now().Add(time.Hour)
	subjectType := "task"
	subjectID := uint64(9)
	_, _, err := normalizeInput(Input{
		DispatchKind: "resume_task", SubjectType: &subjectType, SubjectID: &subjectID,
		Title: "继续任务", Instruction: "重新检查", ScheduleType: "once", RunAt: &runAt,
	}, time.Now(), time.Local)
	if err == nil {
		t.Fatal("public resume_task creation must be rejected")
	}
	input, _, err := normalizeInput(Input{
		DispatchKind: "resume_task", SubjectType: &subjectType, SubjectID: &subjectID,
		Title: "继续任务", Instruction: "重新检查", ScheduleType: "once", RunAt: &runAt,
		initialStatus: "binding",
	}, time.Now(), time.Local)
	if err != nil {
		t.Fatalf("yield-bound resume_task rejected: %v", err)
	}
	if input.DispatchKind != "resume_task" || input.initialStatus != "binding" {
		t.Fatalf("normalized resume input = %#v", input)
	}
}

func TestValidateDeletableRejectsTaskContinuation(t *testing.T) {
	t.Parallel()
	err := validateDeletable(&domain.ScheduledTask{ID: 12, DispatchKind: "resume_task", Status: "active"})
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("validateDeletable() error = %v, want ErrInvalidInput", err)
	}
}

func TestTaskInputUsesStandardM5ApprovalEntry(t *testing.T) {
	t.Parallel()
	row := &domain.ScheduledTask{
		ID: 9, Title: "入会跟进", ActionType: "agent_task",
		Instruction:     "加入指定会议并完成记录",
		ContextSnapshot: []byte(`{"meeting_id":"m_123"}`),
	}
	input, err := taskInput(row, "2026-07-24T01:30:00Z")
	if err != nil {
		t.Fatalf("taskInput() error = %v", err)
	}
	if input.SourceType != taskcreate.SourceScheduledTask || input.SourceID == nil || *input.SourceID != row.ID {
		t.Fatalf("source = %s/%v", input.SourceType, input.SourceID)
	}
	if input.OccurrenceKey == nil {
		t.Fatal("occurrence_key is nil")
	}
	if string(input.SourcePayload) != `{"instruction":"加入指定会议并完成记录"}` {
		t.Fatalf("source_payload = %s", input.SourcePayload)
	}
}

func TestOKRProgressScheduleMaterializesOneIndependentTask(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	submitter := &okrProgressSubmitter{}
	service, err := NewService(db, submitter, okrProgressResumer{}, 10)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	now := time.Date(2026, 8, 27, 4, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	interval := 360
	enabled := true
	schedule, err := service.Create(t.Context(), Input{
		Title: "OKR 只读进展巡检", ActionType: "agent_task",
		Instruction:     "读取 okr-progress-sync Skill；只读 Meego 和已采集消息，更新 Page/Fact；不发送消息。",
		ContextSnapshot: json.RawMessage(`{"skill":"okr-progress-sync","mode":"read_only"}`),
		ScheduleType:    "interval", IntervalMinutes: &interval, Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	triggered, err := service.Trigger(t.Context(), schedule.ID)
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if len(submitter.inputs) != 1 {
		t.Fatalf("submitted tasks = %d, want 1", len(submitter.inputs))
	}
	input := submitter.inputs[0]
	if input.SourceType != taskcreate.SourceScheduledTask || input.SourceID == nil || *input.SourceID != schedule.ID {
		t.Fatalf("Task source = %s/%v, want scheduled_task/%d", input.SourceType, input.SourceID, schedule.ID)
	}
	if string(input.Background) != `{"mode":"read_only","skill":"okr-progress-sync"}` {
		t.Fatalf("Task background = %s", input.Background)
	}
	if strings.Contains(string(input.Background), "okr_id") || strings.Contains(string(input.SourcePayload), "okr_id") {
		t.Fatalf("scheduled Task leaked an OKR relation: background=%s payload=%s", input.Background, input.SourcePayload)
	}
	if triggered.Status != "active" || triggered.LastTaskID == nil || *triggered.LastTaskID != 77 {
		t.Fatalf("triggered schedule = %#v, want active with Task #77", triggered)
	}
	var taskCount int64
	if err := db.Model(&domain.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatalf("count Tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("stubbed schedule persisted %d unexpected Tasks", taskCount)
	}
}

func TestStartSchedulerRejectsInvalidSpec(t *testing.T) {
	t.Parallel()
	service := &Service{}
	if _, err := StartScheduler(t.Context(), service, "not-a-cron", log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("StartScheduler() accepted invalid spec")
	}
}
