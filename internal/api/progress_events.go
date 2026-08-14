package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"jarvis/internal/progress"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func ListTaskEvents(service progress.EventService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40063, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		result, err := service.ListTaskEvents(ctx, taskID)
		if err != nil {
			writeProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": result}})
	}
}

// ListFacts reads one subject's facts. The window is passed as explicit RFC3339
// bounds rather than a calendar date so this handler needs no timezone: whoever
// asks for "today" already knows which timezone they mean.
func ListFacts(service progress.EventService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter := progress.FactFilter{SubjectType: string(c.Query("subject_type"))}
		subjectID, err := strconv.ParseUint(string(c.Query("subject_id")), 10, 64)
		if err != nil || subjectID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40064, fmt.Errorf("subject_id must be a positive integer"))
			return
		}
		filter.SubjectID = subjectID
		if raw := string(c.Query("from")); raw != "" {
			from, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40064, fmt.Errorf("from must be RFC3339: %w", err))
				return
			}
			filter.From = &from
		}
		if raw := string(c.Query("until")); raw != "" {
			until, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40064, fmt.Errorf("until must be RFC3339: %w", err))
				return
			}
			filter.Until = &until
		}
		if raw := string(c.Query("limit")); raw != "" {
			limit, err := strconv.Atoi(raw)
			if err != nil || limit <= 0 {
				writeAPIError(c, consts.StatusBadRequest, 40064, fmt.Errorf("limit must be a positive integer"))
				return
			}
			filter.Limit = limit
		}
		if raw := string(c.Query("source_kind")); raw != "" {
			filter.SourceKind = &raw
		}
		if raw := string(c.Query("exclude_source_kind")); raw != "" {
			filter.ExcludeSourceKind = &raw
		}
		result, err := service.ListFacts(ctx, filter)
		if err != nil {
			writeProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": result}})
	}
}

func AppendFact(service progress.EventService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input progress.FactInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40064, err)
			return
		}
		result, err := service.AppendFact(ctx, input)
		if err != nil {
			writeProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// AppendFacts appends one batch of facts in one request path. The service still
// writes each fact through AppendFact so replay and parent checks remain
// unchanged. Fail-fast is on: the first failed write aborts the batch.
func AppendFacts(service progress.EventService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var inputs []progress.FactInput
		if err := decodeStrictJSON(c.Request.Body(), &inputs); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40064, err)
			return
		}
		if len(inputs) == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40064, fmt.Errorf("%w: at least one fact is required", progress.ErrInvalidInput))
			return
		}
		items := make([]progress.FactView, 0, len(inputs))
		for _, input := range inputs {
			result, err := service.AppendFact(ctx, input)
			if err != nil {
				writeProgressError(c, err)
				return
			}
			items = append(items, *result)
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func writeProgressError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, progress.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40065, err)
	case errors.Is(err, progress.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40461, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50061, err)
	}
}
