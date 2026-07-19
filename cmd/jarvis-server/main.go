// Command jarvis-server 是 Jarvis 的单体主进程（总纲 §1.1）。
//
// 骨架阶段只做三件事：加载配置 → 起 Hertz → 注册路由（/healthz）。
// cron 调度、流水线编排、MySQL/mem0/codex 接入等在后续里程碑逐步加入。
package main

import (
	"flag"

	"jarvis/internal/api"
	"jarvis/internal/config"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

func main() {
	configPath := flag.String("config", "conf/config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		// fail-fast：配置错误启动即暴露，不带缺陷跑起来
		hlog.Fatalf("load config failed: %v", err)
	}

	h := server.New(
		server.WithHostPorts(cfg.Server.Addr),
	)
	api.Register(h)

	hlog.Infof("jarvis-server listening on %s", cfg.Server.Addr)
	// Spin 阻塞运行并处理优雅退出（SIGINT/SIGTERM/SIGHUP）。
	h.Spin()
}
