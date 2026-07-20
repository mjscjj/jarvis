package extract

import (
	"context"
	"fmt"
	"log"

	"github.com/robfig/cron/v3"
)

func StartScheduler(ctx context.Context, worker *Worker, spec string, logger *log.Logger) (*cron.Cron, error) {
	if worker == nil {
		return nil, fmt.Errorf("extract scheduler worker is nil")
	}
	if spec == "" {
		return nil, fmt.Errorf("extract scheduler spec is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("extract scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	))
	if _, err := scheduler.AddFunc(spec, func() {
		stats, err := worker.ExtractOnce(ctx)
		if err != nil {
			logger.Printf("job=extract status=error error=%v", err)
			return
		}
		logger.Printf(
			"job=extract status=ok chats_loaded=%d chats_processed=%d units=%d candidates=%d created=%d updated=%d skipped=%d",
			stats.ChatsLoaded, stats.ChatsProcessed, stats.Units, stats.Candidates, stats.Created, stats.Updated, stats.Skipped,
		)
	}); err != nil {
		return nil, fmt.Errorf("register extract job schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}
