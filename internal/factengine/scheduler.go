package factengine

import (
	"context"
	"fmt"
	"log"

	"jarvis/internal/observability"

	"github.com/robfig/cron/v3"
)

// StartScheduler runs one non-overlapping extraction round on the configured
// cadence. Rounds never overlap: a slow round delays the next one instead of
// racing it for the same watermark.
func StartScheduler(ctx context.Context, worker *Worker, spec string, logger *log.Logger) (*cron.Cron, error) {
	if worker == nil {
		return nil, fmt.Errorf("fact engine scheduler worker is nil")
	}
	if spec == "" {
		return nil, fmt.Errorf("fact engine scheduler spec is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("fact engine scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	))
	if _, err := scheduler.AddFunc(spec, func() {
		jobCtx := observability.EnsureLogID(ctx)
		stats, err := worker.ExtractOnce(jobCtx)
		if err != nil {
			logger.Printf("logid=%s job=extract_facts status=error error=%+v", observability.LogID(jobCtx), err)
			return
		}
		logger.Printf(
			"logid=%s job=extract_facts status=ok units=%d facts=%d rejected=%d last_id=%d",
			observability.LogID(jobCtx), stats.Units, stats.Facts, stats.Rejected, stats.LastID,
		)
	}); err != nil {
		return nil, fmt.Errorf("register fact engine job schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}
