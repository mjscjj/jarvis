package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"jarvis/internal/insight"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type ProactiveRunReader interface {
	ProactiveRuns(context.Context, int) ([]insight.ProactiveRunRow, error)
	ProactiveRun(context.Context, uint64) (*insight.ProactiveRunDetail, error)
}

type MonitoringReader interface {
	Monitoring(context.Context, time.Time, time.Time) (*insight.MonitoringSnapshot, error)
}

func GetDebugMonitoring(service MonitoringReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		from, err := time.Parse(time.RFC3339, string(c.Query("from")))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, fmt.Errorf("from must be RFC3339: %w", err))
			return
		}
		until, err := time.Parse(time.RFC3339, string(c.Query("until")))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40034, fmt.Errorf("until must be RFC3339: %w", err))
			return
		}
		if !from.Before(until) || until.Sub(from) > 8*24*time.Hour {
			writeAPIError(c, consts.StatusBadRequest, 40035, fmt.Errorf("monitoring range must be positive and at most 8 days"))
			return
		}
		snapshot, err := service.Monitoring(ctx, from, until)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50033, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": snapshot})
	}
}

// GetOverview serves the Overview dashboard: live todo/task status counts.
func GetOverview(service *insight.OverviewService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.Load(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50010, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// GetDigests serves the Progress tab: per-day aggregation over the last N days.
func GetDigests(service *insight.DigestService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		days, err := positiveQueryInt(c.Query("days"), 7, "days")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40010, err)
			return
		}
		result, err := service.Load(ctx, days)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40011, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// GetWorklogCommits serves the Progress「项目代码」tab: my MRs across repos for a day.
func GetWorklogCommits(service *insight.WorklogService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.Commits(ctx, string(c.Query("date")))
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50040, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// GetWorklogDocuments serves the Progress「今天的文档」tab: docs I authored /
// received on a day.
func GetWorklogDocuments(service *insight.WorklogService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.Documents(ctx, string(c.Query("date")))
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50041, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// GetDebugScans serves recent capture scan records.
func GetDebugScans(service *insight.DebugService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, err := positiveQueryInt(c.Query("limit"), 50, "limit")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		rows, err := service.Scans(ctx, limit)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50020, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": rows}})
	}
}

// GetDebugWatermarks serves per-chat extraction cursors.
func GetDebugWatermarks(service *insight.DebugService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		rows, err := service.Watermarks(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50021, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": rows}})
	}
}

// GetDebugModules serves the latest cron and realtime pipeline runs by module.
func GetDebugModules(service *insight.DebugService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		lines, err := positiveQueryInt(c.Query("lines"), 1000, "lines")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40023, err)
			return
		}
		rows, err := service.Modules(lines)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50023, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": rows}})
	}
}

// GetDebugAgentProcesses serves the current logical Codex and Trae instances.
func GetDebugAgentProcesses(service *insight.DebugService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		snapshot, err := service.AgentProcesses(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50030, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": snapshot})
	}
}

// GetDebugFailures serves the recent scoped runtime failure timeline.
func GetDebugFailures(service *insight.DebugService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		lines, err := positiveQueryInt(c.Query("lines"), 5000, "lines")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40026, err)
			return
		}
		hours, err := positiveQueryInt(c.Query("hours"), 24, "hours")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40027, err)
			return
		}
		events, err := service.Failures(lines, hours)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50026, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": events}})
	}
}

func GetDebugProactiveRuns(service ProactiveRunReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, err := positiveQueryInt(c.Query("limit"), 50, "limit")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40031, err)
			return
		}
		rows, err := service.ProactiveRuns(ctx, limit)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50031, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": rows}})
	}
}

func GetDebugProactiveRun(service ProactiveRunReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := strconv.ParseUint(c.Param("run_id"), 10, 64)
		if err != nil || id == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40032, fmt.Errorf("run_id must be a positive integer"))
			return
		}
		run, err := service.ProactiveRun(ctx, id)
		if err != nil {
			if errors.Is(err, insight.ErrProactiveRunNotFound) {
				writeAPIError(c, consts.StatusNotFound, 40432, err)
			} else {
				writeAPIError(c, consts.StatusInternalServerError, 50032, err)
			}
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": run})
	}
}

// GetDebugLogs tails the server log file.
func GetDebugLogs(reader *insight.LogReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		lines, err := positiveQueryInt(c.Query("lines"), 300, "lines")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		tail, err := reader.Tail(lines)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50022, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": tail})
	}
}

// GetSystemTaskRuns returns recent executions for one configured scheduler job.
// Records are parsed from the existing process logs; no audit table is created.
func GetSystemTaskRuns(reader *insight.LogReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, err := positiveQueryInt(c.Query("limit"), 100, "limit")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40028, err)
			return
		}
		runs, tail, err := reader.SystemTaskRuns(string(c.Query("job")), limit)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40029, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{
			"items": runs, "sources": tail.Sources, "truncated": tail.Truncated, "notes": tail.Notes,
		}})
	}
}

// SummarizeDigest turns the aggregated digest into prose on demand via codex.
// Returns 503 when the summarizer is not configured (codex disabled).
func SummarizeDigest(service *insight.DigestService, summarizer *insight.Summarizer) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if summarizer == nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50310, fmt.Errorf("digest summarizer is not configured"))
			return
		}
		days, err := positiveQueryInt(c.Query("days"), 7, "days")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40012, err)
			return
		}
		digest, err := service.Load(ctx, days)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40013, err)
			return
		}
		text, err := summarizer.Summarize(ctx, digest)
		if err != nil {
			writeAPIError(c, consts.StatusBadGateway, 50210, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"summary": text, "days": days}})
	}
}
