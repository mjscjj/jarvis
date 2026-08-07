package capture

import (
	"context"
	"fmt"
	"log"
	"time"

	"jarvis/internal/observability"

	"github.com/robfig/cron/v3"
)

// ScheduleConfig contains the two M2 polling schedules. Related chats no longer
// use per-tier cadences: discovery enumerates chats, and a single scan job
// captures every related chat at one uniform interval.
type ScheduleConfig struct {
	Discover string
	Scan     string
}

// StartScheduler registers and starts non-overlapping discovery/scan jobs.
func StartScheduler(ctx context.Context, service *Service, cfg ScheduleConfig, logger *log.Logger) (*cron.Cron, error) {
	if service == nil {
		return nil, fmt.Errorf("capture scheduler service is nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("capture scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	))
	jobs := []struct {
		name string
		spec string
		run  func(context.Context) error
	}{
		{name: "discover", spec: cfg.Discover, run: func(ctx context.Context) error { return service.DiscoverChats(ctx) }},
		{name: "scan_related", spec: cfg.Scan, run: func(ctx context.Context) error {
			return service.ScanPrincipalActivityAndRelated(ctx)
		}},
	}
	for _, job := range jobs {
		job := job
		if job.spec == "" {
			return nil, fmt.Errorf("capture schedule %s is empty", job.name)
		}
		if _, err := scheduler.AddFunc(job.spec, func() {
			jobCtx := observability.EnsureLogID(ctx)
			startedAt := time.Now()
			if err := job.run(jobCtx); err != nil {
				logger.Printf("logid=%s job=%s status=error duration_ms=%d error=%+v", observability.LogID(jobCtx), job.name, time.Since(startedAt).Milliseconds(), err)
				return
			}
			logger.Printf("logid=%s job=%s status=ok duration_ms=%d", observability.LogID(jobCtx), job.name, time.Since(startedAt).Milliseconds())
		}); err != nil {
			return nil, fmt.Errorf("register capture job %s schedule=%q: %w", job.name, job.spec, err)
		}
	}
	scheduler.Start()
	return scheduler, nil
}
