package scheduledtask

import (
	"context"
	"fmt"
	"log"

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
		count, err := service.RunDue(ctx)
		if err != nil {
			logger.Printf("job=scheduled_tasks status=error error=%v", err)
			return
		}
		logger.Printf("job=scheduled_tasks status=ok claimed=%d", count)
	}); err != nil {
		return nil, fmt.Errorf("register scheduled task schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}
