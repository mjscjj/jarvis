package scheduledtask

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWeeklyCalendarBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, zone, after, clock, want string
		weekday                        int
	}{
		{"same day before", "Asia/Shanghai", "2026-09-07T08:00:00+08:00", "09:00", "2026-09-07T09:00:00+08:00", 1},
		{"same day exact", "Asia/Shanghai", "2026-09-07T09:00:00+08:00", "09:00", "2026-09-14T09:00:00+08:00", 1},
		{"same day passed", "Asia/Shanghai", "2026-09-07T09:01:00+08:00", "09:00", "2026-09-14T09:00:00+08:00", 1},
		{"Sunday ISO seven", "Asia/Shanghai", "2026-09-12T20:00:00Z", "09:00", "2026-09-13T09:00:00+08:00", 7},
		{"cross week", "Asia/Shanghai", "2026-09-13T12:00:00+08:00", "09:00", "2026-09-14T09:00:00+08:00", 1},
		{"cross year", "UTC", "2026-12-31T12:00:00Z", "09:00", "2027-01-04T09:00:00Z", 1},
		{"DST starts", "America/New_York", "2026-03-01T09:00:00-05:00", "09:00", "2026-03-08T09:00:00-04:00", 7},
		{"DST ends", "America/New_York", "2026-10-25T09:00:00-04:00", "09:00", "2026-11-01T09:00:00-05:00", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			after, err := time.Parse(time.RFC3339, tc.after)
			if err != nil {
				t.Fatal(err)
			}
			want, err := time.Parse(time.RFC3339, tc.want)
			if err != nil {
				t.Fatal(err)
			}
			input, got, err := normalizeInput(Input{Title: "weekly", Instruction: "run", ScheduleType: "weekly", DailyTime: &tc.clock, Weekday: &tc.weekday}, after, loc)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(want) || got.Location() != time.UTC {
				t.Fatalf("next = %v, want %v", got, want)
			}
			next, err := nextOccurrence(&domain.ScheduledTask{ScheduleType: input.ScheduleType, DailyTime: input.DailyTime, Weekday: input.Weekday}, after, loc)
			if err != nil || !next.Equal(want) {
				t.Fatalf("runtime next = %v, err = %v", next, err)
			}
		})
	}
}

func TestWeeklyValidationAndFieldCleanup(t *testing.T) {
	clock, badClock, day, negative, tooLarge := "09:00", "25:00", 7, 0, 8
	for _, tc := range []struct {
		day   *int
		clock *string
	}{
		{nil, &clock}, {&negative, &clock}, {&tooLarge, &clock}, {&day, nil}, {&day, &badClock},
	} {
		_, _, err := normalizeInput(Input{Title: "weekly", Instruction: "run", ScheduleType: "weekly", DailyTime: tc.clock, Weekday: tc.day}, time.Now(), time.UTC)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected invalid input, got %v", err)
		}
	}
	var fractional Input
	if err := json.Unmarshal([]byte(`{"weekday":1.5}`), &fractional); err == nil {
		t.Fatal("accepted fractional weekday")
	}
	runAt, interval := time.Now(), 10
	for _, kind := range []string{"weekly", "daily", "once", "interval"} {
		input, _, err := normalizeInput(Input{Title: "x", Instruction: "run", ScheduleType: kind, Weekday: &day, DailyTime: &clock, RunAt: &runAt, IntervalMinutes: &interval}, time.Now(), time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		if (input.Weekday != nil) != (kind == "weekly") {
			t.Fatalf("weekday not normalized: %+v", input)
		}
		if kind == "weekly" && (input.RunAt != nil || input.IntervalMinutes != nil) {
			t.Fatalf("weekly fields not cleared: %+v", input)
		}
	}
}

type weeklySubmitter struct{ inputs []taskcreate.Input }

func (s *weeklySubmitter) Submit(_ context.Context, input taskcreate.Input) (*domain.Task, error) {
	s.inputs = append(s.inputs, input)
	return &domain.Task{ID: uint64(len(s.inputs))}, nil
}

func TestWeeklyLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "weekly.db")), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.ScheduledTask{}); err != nil {
		t.Fatal(err)
	}
	service, err := NewCRUDService(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)
	submitter := &weeklySubmitter{}
	service.now = func() time.Time { return now }
	service.location, service.submitter, service.batchLimit = time.UTC, submitter, 10
	ctx := t.Context()
	day, clock := 7, "09:00"
	input := Input{Title: "weekly", Instruction: "汇总上周", ScheduleType: "weekly", Weekday: &day, DailyTime: &clock}
	created, err := service.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Weekday == nil || *created.Weekday != 7 {
		t.Fatalf("Sunday not persisted: %+v", created)
	}
	listed, err := service.List(ctx, ListFilter{Limit: 10})
	if err != nil || len(listed) != 1 || listed[0].Weekday == nil || *listed[0].Weekday != 7 {
		t.Fatalf("list = %+v, err = %v", listed, err)
	}
	manual, err := service.Trigger(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !manual.NextRunAt.Equal(created.NextRunAt) || manual.Status != "active" || len(submitter.inputs) != 1 {
		t.Fatalf("manual trigger changed plan: %+v", manual)
	}

	// Several missed weeks produce one occurrence, not one task per missed week.
	now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if count, err := service.RunDue(ctx); err != nil || count != 1 {
		t.Fatalf("catch-up count=%d err=%v", count, err)
	}
	after, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	if !after.NextRunAt.Equal(want) || after.Status != "active" || len(submitter.inputs) != 2 {
		t.Fatalf("catch-up result: %+v", after)
	}
	if count, err := service.RunDue(ctx); err != nil || count != 0 {
		t.Fatalf("duplicate catch-up count=%d err=%v", count, err)
	}
	if submitter.inputs[1].SourceType != taskcreate.SourceScheduledTask || submitter.inputs[1].OccurrenceKey == nil || *submitter.inputs[1].OccurrenceKey != created.NextRunAt.Format(time.RFC3339Nano) {
		t.Fatalf("lost original occurrence: %+v", submitter.inputs[1])
	}

	// Editing the weekday/time recalculates the plan. Disabled schedules stay idle.
	day, clock = 1, "10:30"
	disabled := false
	input.Enabled = &disabled
	edited, err := service.Update(ctx, created.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	want = time.Date(2026, 10, 5, 10, 30, 0, 0, time.UTC)
	if *edited.Weekday != 1 || !edited.NextRunAt.Equal(want) {
		t.Fatalf("edit = %+v", edited)
	}
	now = want.Add(time.Hour)
	if count, err := service.RunDue(ctx); err != nil || count != 0 {
		t.Fatalf("disabled count=%d err=%v", count, err)
	}

	input.ScheduleType = "daily"
	daily, err := service.Update(ctx, created.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if daily.Weekday != nil || daily.ScheduleType != "daily" {
		t.Fatalf("stale weekday after switch: %+v", daily)
	}
}
