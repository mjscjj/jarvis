package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"jarvis/internal/progress"

	"code.byted.org/middleware/hertz/pkg/app"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
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

func ListProjectEvents(service progress.EventService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		projectID, err := strconv.ParseUint(c.Param("project_id"), 10, 64)
		if err != nil || projectID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40064, fmt.Errorf("project_id must be a positive integer"))
			return
		}
		result, err := service.ListProjectEvents(ctx, projectID)
		if err != nil {
			writeProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": result}})
	}
}

func AppendProjectEvent(service progress.EventService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		projectID, err := strconv.ParseUint(c.Param("project_id"), 10, 64)
		if err != nil || projectID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40064, fmt.Errorf("project_id must be a positive integer"))
			return
		}
		var input progress.ProjectEventInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40064, err)
			return
		}
		input.ProjectID = projectID
		result, err := service.AppendProjectEvent(ctx, input)
		if err != nil {
			writeProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
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
