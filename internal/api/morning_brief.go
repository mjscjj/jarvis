package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/morningbrief"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const (
	defaultMorningBriefLimit = 14
	maxMorningBriefLimit     = 31
)

// MorningBriefService is the read-only API boundary for file-backed briefs.
type MorningBriefService interface {
	List(ctx context.Context, limit int) ([]morningbrief.Brief, error)
}

func ListMorningBriefs(service MorningBriefService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit := defaultMorningBriefLimit
		if raw := strings.TrimSpace(string(c.Query("limit"))); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > maxMorningBriefLimit {
				writeAPIError(c, consts.StatusBadRequest, 40040, fmt.Errorf("limit must be an integer between 1 and %d", maxMorningBriefLimit))
				return
			}
			limit = parsed
		}

		items, err := service.List(ctx, limit)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50040, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}
