package scheduledtask

import (
	"context"
	"fmt"
	"log"

	"jarvis/internal/observability"

	"github.com/robfig/cron/v3"
)

func StartScheduler(ctx context.Context, service *Service, spec string, logger *log.Logger) (*cron.Cron, error) {
	if service == nil {
		return nil, fmt.Errorf("scheduled task scheduler service is nil")
	}
	if spec == "" {
		return nil, fmt.Errorf("scheduled task scheduler spec is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("scheduled task scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(cron.SkipIfStillRunning(cronLogger), cron.Recover(cronLogger)))
	if _, err := scheduler.AddFunc(spec, func() {
		jobCtx := observability.EnsureLogID(ctx)
		count, err := service.RunDue(jobCtx)
		if err != nil {
			logger.Printf("logid=%s job=scheduled_tasks status=error error=%+v", observability.LogID(jobCtx), err)
			return
		}
		logger.Printf("logid=%s job=scheduled_tasks status=ok claimed=%d", observability.LogID(jobCtx), count)
	}); err != nil {
		return nil, fmt.Errorf("register scheduled task schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}
