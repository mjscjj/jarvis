package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"jarvis/internal/worldprogress"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func GetWorldProgress(service worldprogress.AssessmentService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := strconv.ParseUint(c.Param("world_progress_id"), 10, 64)
		if err != nil || id == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40066, fmt.Errorf("world_progress_id must be a positive integer"))
			return
		}
		result, err := service.Get(ctx, id)
		if err != nil {
			writeWorldProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetWorldProgressBySubjectPeriod(service worldprogress.AssessmentService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.GetBySubjectPeriod(ctx, worldprogress.Filter{
			SubjectType: string(c.Query("subject_type")),
			SubjectID:   string(c.Query("subject_id")),
			PeriodKey:   string(c.Query("period_key")),
		})
		if err != nil {
			writeWorldProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func ListWorldProgressByPeriod(service worldprogress.AssessmentService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		items, err := service.ListByPeriod(ctx, c.Param("period_key"))
		if err != nil {
			writeWorldProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func CreateWorldProgress(service worldprogress.AssessmentService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input worldprogress.CreateInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40066, err)
			return
		}
		result, err := service.Create(ctx, input)
		if err != nil {
			writeWorldProgressError(c, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func UpdateWorldProgress(service worldprogress.AssessmentService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := strconv.ParseUint(c.Param("world_progress_id"), 10, 64)
		if err != nil || id == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40066, fmt.Errorf("world_progress_id must be a positive integer"))
			return
		}
		var input worldprogress.UpdateInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40066, err)
			return
		}
		result, err := service.Update(ctx, id, input)
		if err != nil {
			writeWorldProgressError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func writeWorldProgressError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, worldprogress.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40066, err)
	case errors.Is(err, worldprogress.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40462, err)
	case errors.Is(err, worldprogress.ErrConflict):
		writeAPIError(c, consts.StatusConflict, 40962, err)
	case errors.Is(err, worldprogress.ErrSubjectUnavailable):
		writeAPIError(c, consts.StatusConflict, 40963, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50062, err)
	}
}
