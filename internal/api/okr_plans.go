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

func CreateOKRPlanObjective(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var objective okrworkspace.PlanObjectiveView
		if err := decodeStrictJSON(c.Request.Body(), &objective); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40077, err)
			return
		}
		result, err := service.CreatePlanObjective(ctx, strings.TrimSpace(c.Param("plan_id")), objective, currentOKRIdentity(c).OpenID)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40477, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40077, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func UpdateOKRPlanObjective(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.PlanObjectiveWriteInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40078, err)
			return
		}
		planID := strings.TrimSpace(c.Param("plan_id"))
		objectiveID := strings.TrimSpace(c.Param("objective_id"))
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.UpdatePlanObjective(ctx, planID, objectiveID, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetPlan(ctx, planID)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50078, currentErr)
				return
			}
			writeAPIConflict(c, 40978, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40478, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40078, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteOKRPlanObjective(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.PlanObjectiveDeleteInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40079, err)
			return
		}
		planID := strings.TrimSpace(c.Param("plan_id"))
		objectiveID := strings.TrimSpace(c.Param("objective_id"))
		err := service.DeletePlanObjective(ctx, planID, objectiveID, input, currentOKRIdentity(c).OpenID)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetPlan(ctx, planID)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50079, currentErr)
				return
			}
			writeAPIConflict(c, 40979, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40479, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40079, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]string{"id": objectiveID}})
	}
}

func ReorderOKRPlanObjectives(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input struct {
			IDs []string `json:"ids"`
		}
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40080, err)
			return
		}
		planID := strings.TrimSpace(c.Param("plan_id"))
		if err := service.ReorderPlanObjectives(ctx, planID, input.IDs, currentOKRIdentity(c).OpenID); errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40480, err)
			return
		} else if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40080, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"ids": input.IDs}})
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
