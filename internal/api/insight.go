package api

import (
	"context"
	"fmt"

	"jarvis/internal/insight"

	"code.byted.org/middleware/hertz/pkg/app"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
)

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

// GetDebugStatus serves the debug panel health sub-tab.
func GetDebugStatus(service *insight.DebugService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": service.Status(ctx)})
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

// GetDebugModules serves per-module latest cron run parsed from logs.
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

// GetDebugFailures serves the recent cron failure timeline (近 24h 报错时间线)
// so a transient blip that already self-healed is still visible after recovery.
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

// GetDebugTodos serves the newest todos as full rows for JSON inspection.
func GetDebugTodos(service *insight.DebugService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, err := positiveQueryInt(c.Query("limit"), 20, "limit")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40024, err)
			return
		}
		rows, err := service.RecentTodos(ctx, limit)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50024, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": rows}})
	}
}

// GetDebugTasks serves the newest tasks as full rows for JSON inspection.
func GetDebugTasks(service *insight.DebugService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, err := positiveQueryInt(c.Query("limit"), 20, "limit")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40025, err)
			return
		}
		rows, err := service.RecentTasks(ctx, limit)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50025, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": rows}})
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
