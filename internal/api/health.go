package api

import (
	"context"
	"fmt"
	"time"

	"jarvis/internal/observability"

	"code.byted.org/middleware/hertz/pkg/app"
	"code.byted.org/middleware/hertz/pkg/common/hlog"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
	"gorm.io/gorm"
)

// Health reports process and MySQL readiness. Once a dependency is part of the
// startup contract it must be checked here instead of reporting a false green.
func Health(db *gorm.DB) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		ctx = observability.FromRequestContext(ctx, c)
		if db == nil {
			writeHealthError(ctx, c, fmt.Errorf("mysql dependency is nil"))
			return
		}
		sqlDB, err := db.DB()
		if err != nil {
			writeHealthError(ctx, c, fmt.Errorf("get mysql connection: %w", err))
			return
		}
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(pingCtx); err != nil {
			writeHealthError(ctx, c, fmt.Errorf("ping mysql: %w", err))
			return
		}
		c.JSON(consts.StatusOK, healthPayload("ok", ""))
	}
}

func writeHealthError(ctx context.Context, c *app.RequestContext, err error) {
	hlog.CtxErrorf(ctx, "health check failed dependency=mysql error=%+v", err)
	payload := healthPayload("error", err.Error())
	payload["logid"] = observability.LogID(ctx)
	c.JSON(consts.StatusServiceUnavailable, payload)
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
