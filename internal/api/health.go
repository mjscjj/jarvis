package api

import (
	"context"
	"time"

	"code.byted.org/middleware/hertz/pkg/app"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
	"gorm.io/gorm"
)

// Health reports process and MySQL readiness. Once a dependency is part of the
// startup contract it must be checked here instead of reporting a false green.
func Health(db *gorm.DB) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if db == nil {
			c.JSON(consts.StatusServiceUnavailable, healthPayload("error", "mysql dependency is nil"))
			return
		}
		sqlDB, err := db.DB()
		if err != nil {
			c.JSON(consts.StatusServiceUnavailable, healthPayload("error", err.Error()))
			return
		}
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(pingCtx); err != nil {
			c.JSON(consts.StatusServiceUnavailable, healthPayload("error", err.Error()))
			return
		}
		c.JSON(consts.StatusOK, healthPayload("ok", ""))
	}
}

func healthPayload(status, dbError string) map[string]any {
	database := map[string]any{"status": status}
	if dbError != "" {
		database["error"] = dbError
	}
	return map[string]any{
		"status":   status,
		"service":  "jarvis-server",
		"database": database,
		"time":     time.Now().Format(time.RFC3339),
	}
}
