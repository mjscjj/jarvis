package api

import (
	"context"
	"errors"
	"strings"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func regionalScope(c *app.RequestContext) (string, string) {
	return strings.TrimSpace(c.Query("quarter")), strings.TrimSpace(c.Param("region"))
}

func GetRegionalAlignmentBoard(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		quarter, region := regionalScope(c)
		result, err := service.RegionalAlignmentBoard(ctx, quarter, region, currentOKRIdentity(c).OpenID)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40091, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func RefreshRegionalAlignmentBoard(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		quarter, region := regionalScope(c)
		result, err := service.StartRegionalAlignmentRefresh(ctx, quarter, region, currentOKRIdentity(c).OpenID)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40090, err)
			return
		}
		status := consts.StatusOK
		if result.Pending {
			status = consts.StatusAccepted
		}
		c.JSON(status, map[string]any{"code": 0, "data": result})
	}
}

func GetRegionalAlignmentRefreshStatus(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		quarter, region := regionalScope(c)
		result, err := service.RegionalAlignmentRefreshStatus(ctx, quarter, region, currentOKRIdentity(c).OpenID)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40090, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateRegionalDemand(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.RegionalDemandInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40092, err)
			return
		}
		quarter, region := regionalScope(c)
		identity := currentOKRIdentity(c)
		result, err := service.CreateRegionalDemand(ctx, quarter, region, identity.OpenID, input)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40092, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func UpdateRegionalDemand(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.RegionalDemandInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40093, err)
			return
		}
		quarter, region := regionalScope(c)
		identity := currentOKRIdentity(c)
		result, err := service.UpdateRegionalDemand(ctx, quarter, region, strings.TrimSpace(c.Param("demand_id")), identity.OpenID, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			writeAPIConflict(c, 40993, err, nil)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40493, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40093, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteRegionalDemand(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input struct {
			ExpectedVersion int32 `json:"expected_version"`
		}
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40094, err)
			return
		}
		quarter, region := regionalScope(c)
		err := service.DeleteRegionalDemand(ctx, quarter, region, strings.TrimSpace(c.Param("demand_id")), input.ExpectedVersion)
		if errors.Is(err, okrworkspace.ErrConflict) {
			writeAPIConflict(c, 40994, err, nil)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40094, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]string{"id": strings.TrimSpace(c.Param("demand_id"))}})
	}
}

func PutRegionalDecision(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.RegionalPlanDecisionInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40095, err)
			return
		}
		quarter, region := regionalScope(c)
		identity := currentOKRIdentity(c)
		result, err := service.PutRegionalDecision(ctx, quarter, region, strings.TrimSpace(c.Param("kr_id")), identity.OpenID, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			writeAPIConflict(c, 40995, err, nil)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40095, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func PutRegionalSettings(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.RegionalSettingsInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40096, err)
			return
		}
		quarter, region := regionalScope(c)
		identity := currentOKRIdentity(c)
		result, err := service.PutRegionalSettings(ctx, quarter, region, identity.OpenID, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			writeAPIConflict(c, 40996, err, nil)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40096, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func PutRegionalRecapOrder(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.RegionalRecapOrderInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40097, err)
			return
		}
		quarter, region := regionalScope(c)
		identity := currentOKRIdentity(c)
		result, err := service.PutRegionalRecapOrder(ctx, quarter, region, identity.OpenID, input)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40097, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func PatchRegionalRecap(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.RegionalRecapPatchInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40098, err)
			return
		}
		quarter, region := regionalScope(c)
		identity := currentOKRIdentity(c)
		result, err := service.PatchRegionalRecap(ctx, quarter, region, strings.TrimSpace(c.Query("bucket")), strings.TrimSpace(c.Param("objective_id")), identity.OpenID, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			writeAPIConflict(c, 40998, err, nil)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40098, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetRegionalAlignmentComments(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		quarter, region := regionalScope(c)
		result, err := service.AlignmentComments(ctx, quarter, region)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40099, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateRegionalAlignmentComment(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		identity := currentOKRIdentity(c)
		var request createPlanCommentRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40100, err)
			return
		}
		input := okrworkspace.CreateCommentInput{ParentID: request.ParentID, TargetType: request.TargetType, TargetID: request.TargetID, TargetTitle: request.TargetTitle, SelectedText: request.SelectedText, SelectionStart: request.SelectionStart, SelectionEnd: request.SelectionEnd, SelectionPrefix: request.SelectionPrefix, SelectionSuffix: request.SelectionSuffix, AuthorOpenID: identity.OpenID, AuthorUnionID: identity.UnionID, AuthorName: identity.Name, AuthorEmail: identity.Email, Content: request.Content, Mentions: request.Mentions, Images: request.Images}
		quarter, region := regionalScope(c)
		result, err := service.CreateAlignmentComment(ctx, quarter, region, input)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40100, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}
