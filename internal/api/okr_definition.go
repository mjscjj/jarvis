package api

import (
	"context"
	"errors"
	"strings"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func ReplaceKRDefinition(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.ReplaceKRDefinitionInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40046, err)
			return
		}
		id := strings.TrimSpace(c.Param("kr_id"))
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.ReplaceKRDefinition(ctx, id, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetCoreKR(ctx, id)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50046, currentErr)
				return
			}
			writeAPIConflict(c, 40946, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40446, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40046, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func PatchPointDefinition(service *okrworkspace.Service) app.HandlerFunc {
	return patchPointDefinitionWith(func(ctx context.Context, _ string, pointID string, input okrworkspace.PatchPointDefinitionInput) (okrworkspace.PointDefinitionPatchResult, error) {
		return service.PatchPointDefinition(ctx, pointID, input)
	})
}

func PatchPlanPointDefinition(service *okrworkspace.Service) app.HandlerFunc {
	return patchPointDefinitionWith(service.PatchPlanPointDefinition)
}

func patchPointDefinitionWith(patch func(context.Context, string, string, okrworkspace.PatchPointDefinitionInput) (okrworkspace.PointDefinitionPatchResult, error)) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.PatchPointDefinitionInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40047, err)
			return
		}
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := patch(ctx, strings.TrimSpace(c.Param("plan_id")), strings.TrimSpace(c.Param("point_id")), input)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40447, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40047, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
