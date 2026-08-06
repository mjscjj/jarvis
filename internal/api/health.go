package api

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/gorm"
)

// Health reports process and database readiness. Once a dependency is part of the
// startup contract it must be checked here instead of reporting a false green.
func Health(db *gorm.DB) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		ctx = observability.FromRequestContext(ctx, c)
		if db == nil {
			writeHealthError(ctx, c, fmt.Errorf("database dependency is nil"))
			return
		}
		sqlDB, err := db.DB()
		if err != nil {
			writeHealthError(ctx, c, fmt.Errorf("get database connection: %w", err))
			return
		}
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(pingCtx); err != nil {
			writeHealthError(ctx, c, fmt.Errorf("ping database: %w", err))
			return
		}
		c.JSON(consts.StatusOK, healthPayload("ok", ""))
	}
}

// VectorIndexProbe reports Qdrant reachability and the version that answered.
type VectorIndexProbe interface {
	HealthCheck(ctx context.Context) (string, error)
}

// ReadinessTargets are the external dependencies /readyz probes on top of the
// database.
//
// They are kept out of /healthz on purpose: losing one of them degrades a
// feature but leaves the process serving, while rebuild-server.sh polls /healthz
// to decide whether a restart succeeded. Probing a stopped Qdrant there would
// stall that poll past its own timeout and report a failed restart of a server
// that is in fact running.
type ReadinessTargets struct {
	// VectorIndex is nil when semantic dedup is switched off, which reports
	// "disabled" rather than an error.
	VectorIndex VectorIndexProbe
	// LarkCLIBin and AgentCLIBin are looked up the same way the subprocess
	// wrappers resolve them, so a PATH that works for this process but not for
	// launchd shows up here instead of at the first Feishu call.
	LarkCLIBin  string
	AgentCLIBin string
}

// Readiness reports every dependency Jarvis needs to do useful work, so a fresh
// install can tell a missing CLI from a stopped vector store without reading
// logs. Only the database decides the status code, because it is the sole
// dependency the process cannot serve any request without.
func Readiness(db *gorm.DB, targets ReadinessTargets) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		ctx = observability.FromRequestContext(ctx, c)
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		databaseState := probeDatabase(probeCtx, db)
		dependencies := map[string]any{
			"database":     databaseState,
			"vector_index": probeVectorIndex(probeCtx, targets.VectorIndex),
			"lark_cli":     probeBinary(targets.LarkCLIBin),
			"agent_cli":    probeBinary(targets.AgentCLIBin),
		}

		status := consts.StatusOK
		overall := "ok"
		if databaseState["status"] != "ok" {
			overall = "error"
			status = consts.StatusServiceUnavailable
		} else {
			for _, state := range dependencies {
				if value, _ := state.(map[string]any)["status"].(string); value == "error" {
					overall = "degraded"
					break
				}
			}
		}
		if overall != "ok" {
			hlog.CtxWarnf(ctx, "readiness %s dependencies=%+v", overall, dependencies)
		}
		c.JSON(status, map[string]any{
			"status":       overall,
			"service":      "jarvis-server",
			"dependencies": dependencies,
			"logid":        observability.LogID(ctx),
			"time":         time.Now().Format(time.RFC3339),
		})
	}
}

func probeDatabase(ctx context.Context, db *gorm.DB) map[string]any {
	if db == nil {
		return probeFailure(fmt.Errorf("database dependency is nil"))
	}
	sqlDB, err := db.DB()
	if err != nil {
		return probeFailure(fmt.Errorf("get database connection: %w", err))
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return probeFailure(fmt.Errorf("ping database: %w", err))
	}
	return map[string]any{"status": "ok"}
}

func probeVectorIndex(ctx context.Context, index VectorIndexProbe) map[string]any {
	if index == nil {
		return map[string]any{"status": "disabled", "detail": "semantic dedup is off; Todo 语义去重不生效"}
	}
	version, err := index.HealthCheck(ctx)
	if err != nil {
		return probeFailure(err)
	}
	state := map[string]any{"status": "ok"}
	if version != "" {
		state["version"] = version
	}
	return state
}

func probeBinary(bin string) map[string]any {
	if strings.TrimSpace(bin) == "" {
		return probeFailure(fmt.Errorf("binary is not configured"))
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return probeFailure(fmt.Errorf("resolve %q: %w", bin, err))
	}
	return map[string]any{"status": "ok", "path": resolved}
}

func probeFailure(err error) map[string]any {
	return map[string]any{"status": "error", "error": err.Error()}
}

func writeHealthError(ctx context.Context, c *app.RequestContext, err error) {
	hlog.CtxErrorf(ctx, "health check failed dependency=database error=%+v", err)
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
