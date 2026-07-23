package meetingcapture

import (
	"context"
	"fmt"
	"log"

	"jarvis/internal/observability"

	"github.com/robfig/cron/v3"
)

func StartScheduler(ctx context.Context, service *Service, schedule string, logger *log.Logger) (*cron.Cron, error) {
	if service == nil {
		return nil, fmt.Errorf("meeting capture scheduler service is nil")
	}
	if schedule == "" {
		return nil, fmt.Errorf("meeting capture scheduler schedule is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("meeting capture scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(cron.SkipIfStillRunning(cronLogger), cron.Recover(cronLogger)))
	if _, err := scheduler.AddFunc(schedule, func() {
		jobCtx := observability.EnsureLogID(ctx)
		stats, err := service.ScanOnce(jobCtx)
		if err != nil {
			logger.Printf(
				"logid=%s job=meeting_minutes status=error discovered=%d attempted=%d imported=%d waiting=%d permission_denied=%d skipped=%d error=%+v",
				observability.LogID(jobCtx), stats.Discovered, stats.Attempted, stats.Imported, stats.Waiting, stats.PermissionDenied, stats.Skipped, err,
			)
			return
		}
		logger.Printf(
			"logid=%s job=meeting_minutes status=ok discovered=%d attempted=%d imported=%d waiting=%d permission_denied=%d skipped=%d",
			observability.LogID(jobCtx), stats.Discovered, stats.Attempted, stats.Imported, stats.Waiting, stats.PermissionDenied, stats.Skipped,
		)
	}); err != nil {
		return nil, fmt.Errorf("register meeting capture schedule=%q: %w", schedule, err)
	}
	scheduler.Start()
	return scheduler, nil
}
