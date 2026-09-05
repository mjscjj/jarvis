package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func GetFollowUps(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.FollowUps(ctx, strings.TrimSpace(c.Query("quarter")), strings.TrimSpace(c.Query("week")))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40041, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetFollowUp(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.GetFollowUp(ctx, strings.TrimSpace(c.Param("follow_up_id")))
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40442, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40042, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateFollowUp(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.FollowUpInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40043, err)
			return
		}
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.CreateFollowUp(ctx, input)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40043, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func UpdateFollowUp(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id := strings.TrimSpace(c.Param("follow_up_id"))
		if id == "" {
			writeAPIError(c, consts.StatusBadRequest, 40044, fmt.Errorf("follow_up_id is required"))
			return
		}
		var input okrworkspace.FollowUpInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40044, err)
			return
		}
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.UpdateFollowUp(ctx, id, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetFollowUp(ctx, id)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50044, currentErr)
				return
			}
			writeAPIConflict(c, 40944, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40444, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40044, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteFollowUp(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id := strings.TrimSpace(c.Param("follow_up_id"))
		var input okrworkspace.DeleteFollowUpInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40045, err)
			return
		}
		err := service.DeleteFollowUp(ctx, id, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetFollowUp(ctx, id)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50045, currentErr)
				return
			}
			writeAPIConflict(c, 40945, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40445, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40045, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id}})
	}
}
