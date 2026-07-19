package capture

import (
	"context"
	"fmt"
	"log"

	"github.com/robfig/cron/v3"
)

// ScheduleConfig contains the four M2 polling schedules.
type ScheduleConfig struct {
	Discover string
	Hot      string
	Warm     string
	Cold     string
}

// StartScheduler registers and starts non-overlapping discovery/tier jobs.
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
		{name: "scan_hot", spec: cfg.Hot, run: func(ctx context.Context) error { return service.ScanTier(ctx, "hot") }},
		{name: "scan_warm", spec: cfg.Warm, run: func(ctx context.Context) error { return service.ScanTier(ctx, "warm") }},
		{name: "scan_cold", spec: cfg.Cold, run: func(ctx context.Context) error { return service.ScanTier(ctx, "cold") }},
	}
	for _, job := range jobs {
		job := job
		if job.spec == "" {
			return nil, fmt.Errorf("capture schedule %s is empty", job.name)
		}
		if _, err := scheduler.AddFunc(job.spec, func() {
			if err := job.run(ctx); err != nil {
				logger.Printf("job=%s status=error error=%v", job.name, err)
				return
			}
			logger.Printf("job=%s status=ok", job.name)
		}); err != nil {
			return nil, fmt.Errorf("register capture job %s schedule=%q: %w", job.name, job.spec, err)
		}
	}
	scheduler.Start()
	return scheduler, nil
}
