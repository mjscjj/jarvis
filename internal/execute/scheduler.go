package execute

import (
	"context"
	"fmt"
	"log"

	"github.com/robfig/cron/v3"
)

// StartScheduler runs the auto-executor on a cron cadence. It only executes
// local-action Tasks; external actions wait for the manual approve. The batch
// limit bounds how many Tasks one sweep may run.
func StartScheduler(ctx context.Context, executor *AgentExecutor, spec string, batchLimit int, logger *log.Logger) (*cron.Cron, error) {
	if executor == nil {
		return nil, fmt.Errorf("execute scheduler executor is nil")
	}
	if spec == "" {
		return nil, fmt.Errorf("execute scheduler spec is empty")
	}
	if batchLimit <= 0 {
		return nil, fmt.Errorf("execute scheduler batch limit must be positive")
	}
	if logger == nil {
		return nil, fmt.Errorf("execute scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	))
	if _, err := scheduler.AddFunc(spec, func() {
		stats, err := executor.RunPendingBatch(ctx, batchLimit)
		if err != nil {
			logger.Printf("job=execute status=error error=%v", err)
			return
		}
		logger.Printf(
			"job=execute status=ok loaded=%d executed=%d skipped=%d failed=%d",
			stats.Loaded, stats.Executed, stats.Skipped, stats.Failed,
		)
	}); err != nil {
		return nil, fmt.Errorf("register execute job schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}
