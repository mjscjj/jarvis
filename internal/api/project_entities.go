package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/background"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func projectEntityFilter(c *app.RequestContext) (background.ListFilter, uint64, bool, error) {
	filter, err := backgroundListFilter(c)
	if err != nil {
		return filter, 0, false, err
	}
	var projectID uint64
	if raw := strings.TrimSpace(c.Query("project_id")); raw != "" {
		projectID, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || projectID == 0 {
			return filter, 0, false, fmt.Errorf("project_id must be a positive integer")
		}
	}
	includeClosed := false
	if raw := strings.TrimSpace(c.Query("include_closed")); raw != "" {
		includeClosed, err = strconv.ParseBool(raw)
		if err != nil {
			return filter, 0, false, fmt.Errorf("include_closed must be true or false")
		}
	}
	return filter, projectID, includeClosed, nil
}

func ListProjectRisks(svc *background.ProjectRiskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		base, projectID, includeClosed, err := projectEntityFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		result, err := svc.List(ctx, background.ProjectRiskFilter{ListFilter: base, ProjectID: projectID, IncludeClosed: includeClosed})
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateProjectRisk(svc *background.ProjectRiskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in background.ProjectRiskInput
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.Create(ctx, in)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetProjectRisk(svc *background.ProjectRiskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_risk_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		result, err := svc.Get(ctx, id)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func UpdateProjectRisk(svc *background.ProjectRiskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_risk_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		var in background.ProjectRiskInput
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.Update(ctx, id, in)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteProjectRisk(svc *background.ProjectRiskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_risk_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		if err := svc.Delete(ctx, id); err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id, "closed": true}})
	}
}

func ListProjectChanges(svc *background.ProjectChangeService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		base, projectID, includeClosed, err := projectEntityFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		result, err := svc.List(ctx, background.ProjectChangeFilter{ListFilter: base, ProjectID: projectID, IncludeClosed: includeClosed})
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateProjectChange(svc *background.ProjectChangeService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in background.ProjectChangeInput
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.Create(ctx, in)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetProjectChange(svc *background.ProjectChangeService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_change_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		result, err := svc.Get(ctx, id)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func UpdateProjectChange(svc *background.ProjectChangeService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_change_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		var in background.ProjectChangeInput
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.Update(ctx, id, in)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteProjectChange(svc *background.ProjectChangeService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_change_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		if err := svc.Delete(ctx, id); err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id, "closed": true}})
	}
}
