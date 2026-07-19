// Package api 注册 Hertz 路由。管理后台 REST 与各模块内部接口都挂在这里。
package api

import (
	"fmt"

	"jarvis/internal/decide"
	"jarvis/internal/extract"

	"github.com/cloudwego/hertz/pkg/app/server"
	"gorm.io/gorm"
)

// Dependencies are process-level dependencies shared by API handlers.
type Dependencies struct {
	DB                  *gorm.DB
	Todos               extract.TodoReader
	Confirmations       decide.ConfirmationService
	ConfirmationDetails decide.ConfirmationDetailReader
}

// Register 把所有路由挂到 Hertz 实例上。
func Register(h *server.Hertz, deps Dependencies) error {
	if h == nil {
		return fmt.Errorf("api hertz server is nil")
	}
	if deps.DB == nil {
		return fmt.Errorf("api mysql dependency is nil")
	}
	if deps.Todos == nil {
		return fmt.Errorf("api todo reader dependency is nil")
	}
	if deps.Confirmations == nil {
		return fmt.Errorf("api confirmation service dependency is nil")
	}
	if deps.ConfirmationDetails == nil {
		return fmt.Errorf("api confirmation detail reader dependency is nil")
	}
	h.GET("/healthz", Health(deps.DB))
	h.GET("/api/todos", ListTodos(deps.Todos))
	h.GET("/api/todos/:todo_id", GetTodo(deps.Todos))
	h.GET("/api/confirmations", ListConfirmations(deps.Todos))
	h.GET("/api/confirmations/:todo_id", GetConfirmation(deps.ConfirmationDetails))
	h.POST("/api/confirmations/:todo_id/approve", ApproveConfirmation(deps.Confirmations))
	h.POST("/api/confirmations/:todo_id/reject", RejectConfirmation(deps.Confirmations))
	return nil
}
