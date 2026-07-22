package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"jarvis/internal/scheduledtask"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type ScheduledTaskService interface {
	List(context.Context, scheduledtask.ListFilter) ([]scheduledtask.View, error)
	Create(context.Context, scheduledtask.Input) (*scheduledtask.View, error)
	Update(context.Context, uint64, scheduledtask.Input) (*scheduledtask.View, error)
	Delete(context.Context, uint64) error
	Trigger(context.Context, uint64) (*scheduledtask.View, error)
}

func ListScheduledTasks(service ScheduledTaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, err := positiveQueryInt(c.Query("limit"), 200, "limit")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40060, err)
			return
		}
		items, err := service.List(ctx, scheduledtask.ListFilter{Status: c.Query("status"), Limit: limit})
		if err != nil {
			writeScheduledTaskError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func CreateScheduledTask(service ScheduledTaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input scheduledtask.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40061, err)
			return
		}
		view, err := service.Create(ctx, input)
		if err != nil {
			writeScheduledTaskError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func UpdateScheduledTask(service ScheduledTaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := scheduledTaskID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40062, err)
			return
		}
		var input scheduledtask.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40061, err)
			return
		}
		view, err := service.Update(ctx, id, input)
		if err != nil {
			writeScheduledTaskError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func DeleteScheduledTask(service ScheduledTaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := scheduledTaskID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40062, err)
			return
		}
		if err := service.Delete(ctx, id); err != nil {
			writeScheduledTaskError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id, "deleted": true}})
	}
}

func TriggerScheduledTask(service ScheduledTaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := scheduledTaskID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40062, err)
			return
		}
		view, err := service.Trigger(ctx, id)
		if err != nil {
			writeScheduledTaskError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func scheduledTaskID(c *app.RequestContext) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("scheduled_task_id"), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("scheduled_task_id must be a positive integer")
	}
	return id, nil
}

func writeScheduledTaskError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, scheduledtask.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40063, err)
	case errors.Is(err, scheduledtask.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40460, err)
	case errors.Is(err, scheduledtask.ErrRunning):
		writeAPIError(c, consts.StatusConflict, 40960, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50060, err)
	}
}
