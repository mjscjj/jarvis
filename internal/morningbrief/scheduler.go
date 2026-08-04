package morningbrief

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"jarvis/internal/observability"

	"github.com/robfig/cron/v3"
)

type RunOnce interface {
	Run(context.Context, string) (string, error)
	HasBriefFor(day time.Time) bool
}

type Scheduler struct {
	cron      *cron.Cron
	job       cron.Job
	cancel    context.CancelFunc
	started   chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
}

// StartScheduler runs the brief on spec, interpreted in location — the schedule
// itself decides which days and what time count as "morning", so the worker holds
// no weekday or time-of-day rule of its own.
//
// After startupDelay it catches up at most once, and only when today's scheduled
// time has already passed and today's brief is not on disk yet. Without that gate
// every restart would spend a strong-model run, and the first one each day would
// also deliver a "morning" brief at whatever hour the restart happened.
func StartScheduler(ctx context.Context, worker RunOnce, spec string, startupDelay time.Duration, location *time.Location, logger *log.Logger) (*Scheduler, error) {
	if worker == nil {
		return nil, fmt.Errorf("morning brief scheduler worker is nil")
	}
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("morning brief scheduler spec is empty")
	}
	if startupDelay <= 0 {
		return nil, fmt.Errorf("morning brief scheduler startup delay must be positive")
	}
	if location == nil {
		return nil, fmt.Errorf("morning brief scheduler location is nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("morning brief scheduler logger is nil")
	}
	schedule, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, fmt.Errorf("parse morning brief schedule %q: %w", spec, err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	cronLogger := cron.PrintfLogger(logger)
	scheduler := &Scheduler{
		cron: cron.New(cron.WithLocation(location)), cancel: cancel, started: make(chan struct{}),
	}
	scheduler.job = cron.NewChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	).Then(cron.FuncJob(func() {
		jobCtx := observability.EnsureLogID(runCtx)
		result, err := worker.Run(jobCtx, TriggerSchedule)
		if err != nil {
			logger.Printf("logid=%s job=morning_brief status=error error=%+v", observability.LogID(jobCtx), err)
			return
		}
		logger.Printf("logid=%s job=morning_brief status=ok result_chars=%d result=%q", observability.LogID(jobCtx), len(result), result)
	}))
	if _, err := scheduler.cron.AddJob(spec, scheduler.job); err != nil {
		cancel()
		return nil, fmt.Errorf("register morning brief schedule=%q: %w", spec, err)
	}

	go scheduler.startAfter(runCtx, startupDelay, func() {
		now := time.Now().In(location)
		logID := observability.LogID(observability.EnsureLogID(runCtx))
		if !catchUpDue(schedule, now, location) {
			logger.Printf("logid=%s job=morning_brief trigger=startup_catch_up status=skipped reason=not_due_today", logID)
			return
		}
		if worker.HasBriefFor(now) {
			logger.Printf("logid=%s job=morning_brief trigger=startup_catch_up status=skipped reason=brief_already_written", logID)
			return
		}
		scheduler.job.Run()
	})
	return scheduler, nil
}

func (s *Scheduler) startAfter(ctx context.Context, delay time.Duration, catchUp func()) {
	defer close(s.started)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	s.startOnce.Do(s.cron.Start)
	catchUp()
}

// catchUpDue reports whether today's scheduled slot already passed, mirroring
// dailydigest's startup catch-up rule. A weekday-only spec yields false all
// weekend because Next never lands on the current day.
func catchUpDue(schedule cron.Schedule, now time.Time, location *time.Location) bool {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	todayRun := schedule.Next(dayStart.Add(-time.Nanosecond)).In(location)
	year, month, day := todayRun.Date()
	startYear, startMonth, startDay := dayStart.Date()
	if year != startYear || month != startMonth || day != startDay {
		return false
	}
	return !todayRun.After(now)
}

func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.cancel()
		<-s.started
		stopCtx := s.cron.Stop()
		<-stopCtx.Done()
	})
}
