package api

import (
	"context"
	"errors"
	"strings"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func ListOKRPlans(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.ListPlans(ctx, strings.TrimSpace(c.Query("quarter")))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40072, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetOKRPlan(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.GetPlan(ctx, strings.TrimSpace(c.Param("plan_id")))
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40473, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40073, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateOKRPlan(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.CreatePlanInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40074, err)
			return
		}
		input.CreatedBy = currentOKRIdentity(c).OpenID
		result, err := service.CreatePlan(ctx, input)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40074, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func ReplaceOKRPlan(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.ReplacePlanInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40075, err)
			return
		}
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.ReplacePlan(ctx, strings.TrimSpace(c.Param("plan_id")), input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetPlan(ctx, strings.TrimSpace(c.Param("plan_id")))
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50075, currentErr)
				return
			}
			writeAPIConflict(c, 40975, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40475, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40075, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteOKRPlan(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id := strings.TrimSpace(c.Param("plan_id"))
		if err := service.DeletePlan(ctx, id); errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40476, err)
			return
		} else if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40076, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]string{"id": id}})
	}
}
