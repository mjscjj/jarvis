package meetingsweep

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
}

type Scheduler struct {
	cron      *cron.Cron
	job       cron.Job
	cancel    context.CancelFunc
	started   chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
}

// StartScheduler waits for startupDelay before both the first sweep and the
// recurring schedule begin. This gives Jarvis's HTTP API time to become
// available before the agent invokes jarvis-tools append-clue.
func StartScheduler(ctx context.Context, worker RunOnce, spec string, startupDelay time.Duration, logger *log.Logger) (*Scheduler, error) {
	if worker == nil {
		return nil, fmt.Errorf("meeting sweep scheduler worker is nil")
	}
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("meeting sweep scheduler spec is empty")
	}
	if startupDelay <= 0 {
		return nil, fmt.Errorf("meeting sweep scheduler startup delay must be positive")
	}
	if logger == nil {
		return nil, fmt.Errorf("meeting sweep scheduler logger is nil")
	}
	if _, err := cron.ParseStandard(spec); err != nil {
		return nil, fmt.Errorf("parse meeting sweep schedule %q: %w", spec, err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	cronLogger := cron.PrintfLogger(logger)
	scheduler := &Scheduler{
		cron: cron.New(), cancel: cancel, started: make(chan struct{}),
	}
	scheduler.job = cron.NewChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	).Then(cron.FuncJob(func() {
		jobCtx := observability.EnsureLogID(runCtx)
		result, err := worker.Run(jobCtx, TriggerSchedule)
		if err != nil {
			logger.Printf("logid=%s job=meeting_sweep status=error error=%+v", observability.LogID(jobCtx), err)
			return
		}
		logger.Printf("logid=%s job=meeting_sweep status=ok result_chars=%d result=%q", observability.LogID(jobCtx), len(result), result)
	}))
	if _, err := scheduler.cron.AddJob(spec, scheduler.job); err != nil {
		cancel()
		return nil, fmt.Errorf("register meeting sweep schedule=%q: %w", spec, err)
	}

	go scheduler.startAfter(runCtx, startupDelay)
	return scheduler, nil
}

func (s *Scheduler) startAfter(ctx context.Context, delay time.Duration) {
	defer close(s.started)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	s.startOnce.Do(s.cron.Start)
	s.job.Run()
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
