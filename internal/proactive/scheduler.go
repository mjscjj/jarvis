package proactive

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

// StartScheduler waits for startupDelay before both the first review and the
// recurring schedule begin. This gives Jarvis's HTTP API and Task notifier time
// to become available before the agent invokes jarvis-tools.
func StartScheduler(ctx context.Context, worker RunOnce, spec string, startupDelay time.Duration, logger *log.Logger) (*Scheduler, error) {
	if worker == nil {
		return nil, fmt.Errorf("proactive scheduler worker is nil")
	}
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("proactive scheduler spec is empty")
	}
	if startupDelay <= 0 {
		return nil, fmt.Errorf("proactive scheduler startup delay must be positive")
	}
	if logger == nil {
		return nil, fmt.Errorf("proactive scheduler logger is nil")
	}
	if _, err := cron.ParseStandard(spec); err != nil {
		return nil, fmt.Errorf("parse proactive schedule %q: %w", spec, err)
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
		startedAt := time.Now()
		result, err := worker.Run(jobCtx, TriggerSchedule)
		if err != nil {
			logger.Printf("logid=%s job=proactive_heartbeat status=error duration_ms=%d error=%+v", observability.LogID(jobCtx), time.Since(startedAt).Milliseconds(), err)
			return
		}
		logger.Printf("logid=%s job=proactive_heartbeat status=ok duration_ms=%d result_chars=%d result=%q", observability.LogID(jobCtx), time.Since(startedAt).Milliseconds(), len(result), result)
	}))
	if _, err := scheduler.cron.AddJob(spec, scheduler.job); err != nil {
		cancel()
		return nil, fmt.Errorf("register proactive schedule=%q: %w", spec, err)
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
