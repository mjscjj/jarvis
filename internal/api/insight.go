package api

import (
	"context"
	"fmt"

	"jarvis/internal/insight"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
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
