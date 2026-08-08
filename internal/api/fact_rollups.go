package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/factengine"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// FactRollupGenerator compresses one natural day's detail facts into rollups.
type FactRollupGenerator interface {
	RollupDay(context.Context, time.Time) (factengine.RollupStats, error)
	RollupSubjectDay(context.Context, time.Time, string, uint64) (factengine.RollupStats, error)
}

type generateFactRollupRequest struct {
	Date        string `json:"date"`
	SubjectType string `json:"subject_type"`
	SubjectID   uint64 `json:"subject_id"`
}

// GenerateFactRollups manually compresses one local calendar day. The date is
// interpreted in the server's configured capture timezone (same location the
// scheduler uses); the client passes YYYY-MM-DD, not a timezone offset.
func GenerateFactRollups(generator FactRollupGenerator, location *time.Location) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if generator == nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50370, fmt.Errorf("fact rollup generator is not enabled"))
			return
		}
		if location == nil {
			writeAPIError(c, consts.StatusInternalServerError, 50070, fmt.Errorf("fact rollup location is nil"))
			return
		}
		var req generateFactRollupRequest
		if err := decodeStrictJSON(c.Request.Body(), &req); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40070, err)
			return
		}
		date := strings.TrimSpace(req.Date)
		if date == "" {
			writeAPIError(c, consts.StatusBadRequest, 40070, fmt.Errorf("date is required (YYYY-MM-DD)"))
			return
		}
		dayStart, err := time.ParseInLocation("2006-01-02", date, location)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40070, fmt.Errorf("date must be YYYY-MM-DD: %w", err))
			return
		}
		req.SubjectType = strings.TrimSpace(strings.ToLower(req.SubjectType))
		if (req.SubjectType == "") != (req.SubjectID == 0) {
			writeAPIError(c, consts.StatusBadRequest, 40070, fmt.Errorf("subject_type and subject_id must be provided together"))
			return
		}
		var stats factengine.RollupStats
		if req.SubjectType != "" {
			stats, err = generator.RollupSubjectDay(ctx, dayStart, req.SubjectType, req.SubjectID)
		} else {
			stats, err = generator.RollupDay(ctx, dayStart)
		}
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50070, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": stats})
	}
}
