// Package api 注册 Hertz 路由。管理后台 REST 与各模块内部接口都挂在这里。
package api

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	"gorm.io/gorm"
)

// Dependencies are process-level dependencies shared by API handlers.
type Dependencies struct {
	DB *gorm.DB
}

// Register 把所有路由挂到 Hertz 实例上。
// 骨架阶段只有 /healthz；后续各模块（M0 后台、M2~M5）在此扩展分组路由。
func Register(h *server.Hertz, deps Dependencies) {
	h.GET("/healthz", Health(deps.DB))
}
