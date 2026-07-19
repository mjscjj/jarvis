package api

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// Health 存活探针。骨架阶段只报进程存活，不探 MySQL/mem0/codex 等下游
// （下游健康检查待各模块接入后再加，避免此时误报不健康）。
func Health(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]any{
		"status":  "ok",
		"service": "jarvis-server",
		"time":    time.Now().Format(time.RFC3339),
	})
}
