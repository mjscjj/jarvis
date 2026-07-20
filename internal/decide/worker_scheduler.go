package decide

import (
	"context"
	"fmt"
	"log"

	"github.com/robfig/cron/v3"
)

func StartWorkerScheduler(ctx context.Context, worker *DecisionWorker, spec string, logger *log.Logger) (*cron.Cron, error) {
	if worker == nil {
		return nil, fmt.Errorf("decision scheduler worker is nil")
	}
	if spec == "" {
		return nil, fmt.Errorf("decision scheduler spec is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("decision scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	))
	if _, err := scheduler.AddFunc(spec, func() {
		stats, err := worker.EvaluateOnce(ctx)
		if err != nil {
			logger.Printf("job=decide status=error error=%v", err)
			return
		}
		logger.Printf(
			"job=decide status=ok loaded=%d evaluated=%d auto=%d need_info=%d need_decision=%d dropped=%d",
			stats.Loaded, stats.Evaluated, stats.Auto, stats.NeedInfo, stats.NeedDecision, stats.Dropped,
		)
	}); err != nil {
		return nil, fmt.Errorf("register decision job schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}
