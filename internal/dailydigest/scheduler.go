package dailydigest

import (
	"context"
	"fmt"
	"log"

	"github.com/robfig/cron/v3"
)

// StartScheduler 按配置的 cron 表达式（默认每晚 19:00）跑一次当天全量生成：个人
// 1 条 + 每个关键群各 1 条。照 internal/memory/scheduler.go：SkipIfStillRunning
// 防重入 + Recover 防 panic 打挂 cron。返回的 *cron.Cron 由调用方负责 Stop。
func StartScheduler(ctx context.Context, service *Service, spec string, logger *log.Logger) (*cron.Cron, error) {
	if service == nil {
		return nil, fmt.Errorf("daily digest scheduler service is nil")
	}
	if spec == "" {
		return nil, fmt.Errorf("daily digest scheduler spec is empty")
	}
	if logger == nil {
		return nil, fmt.Errorf("daily digest scheduler logger is nil")
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	))
	if _, err := scheduler.AddFunc(spec, func() {
		date := service.today()
		if err := service.GenerateForDate(ctx, date); err != nil {
			logger.Printf("job=daily_digest status=error date=%s error=%v", date, err)
			return
		}
		logger.Printf("job=daily_digest status=ok date=%s", date)
	}); err != nil {
		return nil, fmt.Errorf("register daily digest job schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	return scheduler, nil
}
