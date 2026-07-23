package memory

import (
	"context"
	"fmt"
	"log"

	"jarvis/internal/observability"

	"github.com/robfig/cron/v3"
)

// StartScheduler runs one non-overlapping memory job on the configured cadence.
func StartScheduler(ctx context.Context, worker *Worker, spec string, logger *log.Logger) (*cron.Cron, error) {
	if worker == nil {
		return nil, fmt.Errorf("memory scheduler worker is nil")
	}
	if spec == "" {
		return nil, fmt.Errorf("memory scheduler spec is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("memory scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	))
	if _, err := scheduler.AddFunc(spec, func() {
		jobCtx := observability.EnsureLogID(ctx)
		stats, err := worker.MemorizeOnce(jobCtx)
		if err != nil {
			logger.Printf("logid=%s job=memorize status=error error=%+v", observability.LogID(jobCtx), err)
			return
		}
		logger.Printf(
			"logid=%s job=memorize status=ok loaded=%d processed=%d memorized=%d skipped=%d windows=%d",
			observability.LogID(jobCtx), stats.Loaded, stats.Processed, stats.MemorizedMessages, stats.SkippedMessages, stats.Windows,
		)
	}); err != nil {
		return nil, fmt.Errorf("register memory job schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}
