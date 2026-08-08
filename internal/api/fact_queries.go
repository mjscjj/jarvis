package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/progress"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func FactTimeline(service progress.FactQueryService, location *time.Location) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if location == nil {
			writeAPIError(c, consts.StatusInternalServerError, 50072, fmt.Errorf("fact timeline location is nil"))
			return
		}
		filter := progress.FactTimelineFilter{Days: 3, Location: location, SubjectType: string(c.Query("subject_type"))}
		if raw := string(c.Query("days")); raw != "" {
			days, err := strconv.Atoi(raw)
			if err != nil || days <= 0 || days > 31 {
				writeAPIError(c, consts.StatusBadRequest, 40072, fmt.Errorf("days must be between 1 and 31"))
				return
			}
			filter.Days = days
		}
		if raw := string(c.Query("subject_id")); raw != "" {
			subjectID, err := strconv.ParseUint(raw, 10, 64)
			if err != nil || subjectID == 0 {
				writeAPIError(c, consts.StatusBadRequest, 40072, fmt.Errorf("subject_id must be a positive integer"))
				return
			}
			filter.SubjectID = subjectID
		}
		result, err := service.FactTimeline(ctx, filter)
		if err != nil {
			writeProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func SearchFacts(service progress.FactQueryService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter := progress.FactSearchFilter{
			Query:       string(c.Query("q")),
			SubjectType: string(c.Query("subject_type")),
			SourceKind:  string(c.Query("source_kind")),
			Layer:       string(c.Query("layer")),
			Page:        1,
			PageSize:    50,
		}
		var err error
		if raw := string(c.Query("from")); raw != "" {
			from, parseErr := time.Parse(time.RFC3339, raw)
			if parseErr != nil {
				writeAPIError(c, consts.StatusBadRequest, 40073, fmt.Errorf("from must be RFC3339: %w", parseErr))
				return
			}
			filter.From = &from
		}
		if raw := string(c.Query("until")); raw != "" {
			until, parseErr := time.Parse(time.RFC3339, raw)
			if parseErr != nil {
				writeAPIError(c, consts.StatusBadRequest, 40073, fmt.Errorf("until must be RFC3339: %w", parseErr))
				return
			}
			filter.Until = &until
		}
		if raw := string(c.Query("subject_id")); raw != "" {
			filter.SubjectID, err = strconv.ParseUint(raw, 10, 64)
			if err != nil || filter.SubjectID == 0 {
				writeAPIError(c, consts.StatusBadRequest, 40073, fmt.Errorf("subject_id must be a positive integer"))
				return
			}
		}
		if raw := string(c.Query("page")); raw != "" {
			filter.Page, err = strconv.Atoi(raw)
			if err != nil || filter.Page <= 0 {
				writeAPIError(c, consts.StatusBadRequest, 40073, fmt.Errorf("page must be a positive integer"))
				return
			}
		}
		if raw := string(c.Query("page_size")); raw != "" {
			filter.PageSize, err = strconv.Atoi(raw)
			if err != nil || filter.PageSize <= 0 || filter.PageSize > 200 {
				writeAPIError(c, consts.StatusBadRequest, 40073, fmt.Errorf("page_size must be between 1 and 200"))
				return
			}
		}
		filter.Layer = strings.TrimSpace(filter.Layer)
		result, err := service.SearchFacts(ctx, filter)
		if err != nil {
			writeProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
