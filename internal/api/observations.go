package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/observe"

	"code.byted.org/middleware/hertz/pkg/app"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
)

func ListObservations(service observe.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter, err := observationFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40070, err)
			return
		}
		result, err := service.List(ctx, filter)
		if err != nil {
			writeObservationError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteObservation(service observe.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := strconv.ParseUint(c.Param("observation_id"), 10, 64)
		if err != nil || id == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40071, fmt.Errorf("observation_id must be a positive integer"))
			return
		}
		if err := service.Delete(ctx, id); err != nil {
			writeObservationError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id, "deleted": true}})
	}
}

func observationFilter(c *app.RequestContext) (observe.Filter, error) {
	page, err := positiveQueryInt(c.Query("page"), 1, "page")
	if err != nil {
		return observe.Filter{}, err
	}
	pageSize, err := positiveQueryInt(c.Query("page_size"), 20, "page_size")
	if err != nil {
		return observe.Filter{}, err
	}
	filter := observe.Filter{
		Producer: strings.TrimSpace(c.Query("producer")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Page:     page,
		PageSize: pageSize,
	}
	if raw := strings.TrimSpace(c.Query("project_id")); raw != "" {
		projectID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || projectID == 0 {
			return observe.Filter{}, fmt.Errorf("project_id must be a positive integer")
		}
		filter.ProjectID = &projectID
	}
	return filter, nil
}

func writeObservationError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, observe.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40072, err)
	case errors.Is(err, observe.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40470, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50070, err)
	}
}
