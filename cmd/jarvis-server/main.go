// Command jarvis-server 是 Jarvis 的单体主进程（总纲 §1.1）。
//
// 当前启动链路：加载配置 → 连接 MySQL → 迁移核心表 → 起 Hertz。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"jarvis/internal/api"
	"jarvis/internal/capture"
	"jarvis/internal/config"
	"jarvis/internal/larkcli"
	"jarvis/internal/store"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

func main() {
	configPath := flag.String("config", "conf/config.yaml", "配置文件路径")
	migrateOnly := flag.Bool("migrate-only", false, "只执行数据库迁移，成功后退出")
	discoverOnce := flag.Bool("discover-once", false, "执行一次飞书会话发现，成功后退出")
	scanChat := flag.String("scan-chat", "", "增量扫描指定飞书 chat_id，成功后退出")
	setRelatedGroups := flag.String("set-related-groups", "", "用逗号分隔的 chat_id 原子替换 related_group，成功后退出")
	flag.Parse()
	actionCount := 0
	for _, selected := range []bool{*migrateOnly, *discoverOnce, *scanChat != "", *setRelatedGroups != ""} {
		if selected {
			actionCount++
		}
	}
	if actionCount > 1 {
		hlog.Fatalf("-migrate-only, -discover-once, -scan-chat and -set-related-groups are mutually exclusive")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		// fail-fast：配置错误启动即暴露，不带缺陷跑起来
		hlog.Fatalf("load config failed: %v", err)
	}

	connectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := store.OpenMySQL(connectCtx, cfg.MySQL)
	if err != nil {
		hlog.Fatalf("connect mysql failed: %v", err)
	}
	defer func() {
		if err := store.Close(db); err != nil {
			hlog.Errorf("close mysql failed: %v", err)
		}
	}()

	if err := store.Migrate(db); err != nil {
		hlog.Fatalf("migrate mysql failed: %v", err)
	}
	if *migrateOnly {
		hlog.Infof("mysql schema migration completed")
		return
	}

	larkClient, err := larkcli.New(larkcli.Options{
		Bin:         cfg.LarkCLI.Bin,
		RateLimit:   cfg.LarkCLI.RateLimit,
		Burst:       cfg.LarkCLI.Burst,
		Concurrency: cfg.LarkCLI.Concurrent,
		Timeout:     time.Duration(cfg.LarkCLI.TimeoutSec) * time.Second,
	})
	if err != nil {
		hlog.Fatalf("initialize lark-cli failed: %v", err)
	}
	location, err := time.LoadLocation(cfg.Capture.Timezone)
	if err != nil {
		hlog.Fatalf("load capture timezone failed: %v", err)
	}
	captureService, err := capture.NewService(db, larkClient, capture.Options{
		PageSize:    cfg.Capture.PageSize,
		ScanWorkers: cfg.Capture.ScanWorkers,
		HotAge:      time.Duration(cfg.Capture.HotAgeHours) * time.Hour,
		WarmAge:     time.Duration(cfg.Capture.WarmAgeHours) * time.Hour,
		Location:    location,
	})
	if err != nil {
		hlog.Fatalf("initialize capture service failed: %v", err)
	}
	if *discoverOnce {
		if err := captureService.DiscoverChats(context.Background()); err != nil {
			hlog.Fatalf("discover chats failed: %v", err)
		}
		hlog.Infof("chat discovery completed")
		return
	}
	if *scanChat != "" {
		if err := captureService.ScanChat(context.Background(), *scanChat); err != nil {
			hlog.Fatalf("scan chat failed: %v", err)
		}
		hlog.Infof("chat scan completed: %s", *scanChat)
		return
	}
	if *setRelatedGroups != "" {
		if err := captureService.ReplaceRelatedGroups(strings.Split(*setRelatedGroups, ",")); err != nil {
			hlog.Fatalf("set related groups failed: %v", err)
		}
		hlog.Infof("related groups replaced")
		return
	}
	captureCtx, cancelCapture := context.WithCancel(context.Background())
	defer cancelCapture()
	scheduler, err := capture.StartScheduler(captureCtx, captureService, capture.ScheduleConfig{
		Discover: cfg.Capture.DiscoverSchedule,
		Hot:      cfg.Capture.HotSchedule,
		Warm:     cfg.Capture.WarmSchedule,
		Cold:     cfg.Capture.ColdSchedule,
	}, log.New(os.Stderr, "capture-cron ", log.LstdFlags|log.Lmicroseconds))
	if err != nil {
		hlog.Fatalf("start capture scheduler failed: %v", err)
	}
	defer func() {
		cancelCapture()
		<-scheduler.Stop().Done()
	}()

	h := server.New(
		server.WithHostPorts(cfg.Server.Addr),
	)
	api.Register(h, api.Dependencies{DB: db})

	hlog.Infof("jarvis-server listening on %s", cfg.Server.Addr)
	// Spin 阻塞运行并处理优雅退出（SIGINT/SIGTERM/SIGHUP）。
	h.Spin()
}
