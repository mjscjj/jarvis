package pipeline

import (
	"context"
	"fmt"
	"log"

	"jarvis/internal/observability"

	"github.com/robfig/cron/v3"
)

type ScheduleConfig struct {
	Extract string
	Decide  string
	Execute string
}

// StartScheduler registers only compensation wake-ups. The coordinator remains
// the sole automatic caller of M3/M4/M5 for both real-time and scheduled work.
func StartScheduler(ctx context.Context, coordinator *Coordinator, cfg ScheduleConfig, logger *log.Logger) (*cron.Cron, error) {
	if coordinator == nil {
		return nil, fmt.Errorf("pipeline scheduler coordinator is nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("pipeline scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(cron.SkipIfStillRunning(cronLogger), cron.Recover(cronLogger)))
	jobs := []struct {
		name    string
		spec    string
		enabled bool
		run     func(context.Context) error
	}{
		{name: "extract_reconcile", spec: cfg.Extract, enabled: coordinator.extractor != nil, run: coordinator.ReconcileExtract},
		{name: "decide_reconcile", spec: cfg.Decide, enabled: coordinator.decider != nil, run: coordinator.ReconcileDecide},
		{name: "execute_reconcile", spec: cfg.Execute, enabled: coordinator.executor != nil, run: coordinator.ReconcileExecute},
	}
	for _, job := range jobs {
		job := job
		if !job.enabled {
			continue
		}
		if job.spec == "" {
			return nil, fmt.Errorf("pipeline schedule %s is empty", job.name)
		}
		if _, err := scheduler.AddFunc(job.spec, func() {
			jobCtx := observability.EnsureLogID(ctx)
			if err := job.run(jobCtx); err != nil {
				logger.Printf("logid=%s job=%s status=error error=%+v", observability.LogID(jobCtx), job.name, err)
				return
			}
			logger.Printf("logid=%s job=%s status=queued", observability.LogID(jobCtx), job.name)
		}); err != nil {
			return nil, fmt.Errorf("register pipeline job %s schedule=%q: %w", job.name, job.spec, err)
		}
	}
	scheduler.Start()
	return scheduler, nil
}
