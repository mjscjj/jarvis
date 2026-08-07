package dailydigest

import (
	"context"
	"fmt"
	"log"
	"time"

	"jarvis/internal/observability"

	"github.com/robfig/cron/v3"
)

// StartScheduler 按配置的 cron 表达式只生成个人总结。群总结保留手动入口，不再
// 与个人 Codex 串行耦合。启动时间晚于当天计划点且当天从未尝试时会补跑一次；
// 已有失败记录不会因重启反复自动消耗，需用户手动重试。
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
	schedule, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, fmt.Errorf("parse daily digest schedule %q: %w", spec, err)
	}
	cronLogger := cron.PrintfLogger(logger)
	scheduler := cron.New(cron.WithLocation(service.location), cron.WithChain(
		cron.SkipIfStillRunning(cronLogger),
		cron.Recover(cronLogger),
	))
	if _, err := scheduler.AddFunc(spec, func() {
		runScheduledPersonalDigest(observability.EnsureLogID(ctx), service, logger, "cron")
	}); err != nil {
		return nil, fmt.Errorf("register daily digest job schedule=%q: %w", spec, err)
	}
	scheduler.Start()
	if catchUpDue(schedule, service.now().In(service.location), service.location) {
		go runScheduledPersonalDigest(observability.EnsureLogID(ctx), service, logger, "startup_catch_up")
	}
	return scheduler, nil
}

func runScheduledPersonalDigest(ctx context.Context, service *Service, logger *log.Logger, reason string) {
	startedAt := time.Now()
	date := service.today()
	generated, err := service.GeneratePersonalScheduled(ctx, date)
	if err != nil {
		logger.Printf("logid=%s job=personal_daily_digest trigger=%s status=error duration_ms=%d date=%s error=%+v", observability.LogID(ctx), reason, time.Since(startedAt).Milliseconds(), date, err)
		return
	}
	if !generated {
		logger.Printf("logid=%s job=personal_daily_digest trigger=%s status=skipped duration_ms=%d date=%s reason=already_attempted", observability.LogID(ctx), reason, time.Since(startedAt).Milliseconds(), date)
		return
	}
	logger.Printf("logid=%s job=personal_daily_digest trigger=%s status=ok duration_ms=%d date=%s", observability.LogID(ctx), reason, time.Since(startedAt).Milliseconds(), date)
}

func catchUpDue(schedule cron.Schedule, now time.Time, location *time.Location) bool {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	todayRun := schedule.Next(dayStart.Add(-time.Nanosecond)).In(location)
	return sameLocalDate(todayRun, dayStart) && !todayRun.After(now)
}

func sameLocalDate(left, right time.Time) bool {
	leftYear, leftMonth, leftDay := left.Date()
	rightYear, rightMonth, rightDay := right.Date()
	return leftYear == rightYear && leftMonth == rightMonth && leftDay == rightDay
}
