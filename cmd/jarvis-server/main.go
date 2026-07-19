// Command jarvis-server 是 Jarvis 的单体主进程（总纲 §1.1）。
//
// 当前启动链路：加载配置 → 连接 MySQL → 迁移核心表 → 起 Hertz。
package main

import (
	"context"
	"flag"
	"time"

	"jarvis/internal/api"
	"jarvis/internal/config"
	"jarvis/internal/store"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

func main() {
	configPath := flag.String("config", "conf/config.yaml", "配置文件路径")
	migrateOnly := flag.Bool("migrate-only", false, "只执行数据库迁移，成功后退出")
	flag.Parse()

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

	if err := store.MigrateCore(db); err != nil {
		hlog.Fatalf("migrate mysql failed: %v", err)
	}
	if *migrateOnly {
		hlog.Infof("mysql core schema migration completed")
		return
	}

	h := server.New(
		server.WithHostPorts(cfg.Server.Addr),
	)
	api.Register(h, api.Dependencies{DB: db})

	hlog.Infof("jarvis-server listening on %s", cfg.Server.Addr)
	// Spin 阻塞运行并处理优雅退出（SIGINT/SIGTERM/SIGHUP）。
	h.Spin()
}
