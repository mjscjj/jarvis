package execute

import (
	"context"
	"fmt"
	"log"

	"github.com/robfig/cron/v3"
)

// StartScheduler runs the auto-executor on a cron cadence. Local actions run to
// completion; external actions run the propose stage and, when high-risk, park
// at awaiting_approval for a human — cron never lands a high-risk external write
// on its own. The batch limit bounds how many Tasks one sweep may run.
func StartScheduler(ctx context.Context, executor *AgentExecutor, spec string, batchLimit, concurrency int, logger *log.Logger) (*cron.Cron, error) {
	if executor == nil {
		return nil, fmt.Errorf("execute scheduler executor is nil")
	}
	if spec == "" {
		return nil, fmt.Errorf("execute scheduler spec is empty")
	}
	if batchLimit <= 0 {
		return nil, fmt.Errorf("execute scheduler batch limit must be positive")
	}
	if concurrency <= 0 {
		return nil, fmt.Errorf("execute scheduler concurrency must be positive")
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
		stats, err := executor.RunPendingBatch(ctx, batchLimit, concurrency)
		if err != nil {
			logger.Printf("job=execute status=error error=%v", err)
			return
		}
		logger.Printf(
			"job=execute status=ok loaded=%d executed=%d awaiting_approval=%d failed=%d stale_failed=%d",
			stats.Loaded, stats.Executed, stats.AwaitingApproval, stats.Failed, stats.StaleFailed,
		)
	}); err != nil {
		return nil, fmt.Errorf("register execute job schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}
