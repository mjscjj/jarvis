package api

import (
	"context"
	"errors"

	"jarvis/internal/background"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func ExportProject(svc *background.ProjectPortabilityService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		result, err := svc.Export(ctx, id)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func PreviewProjectImport(svc *background.ProjectPortabilityService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input background.ProjectImportInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.Preview(ctx, input)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func ImportProject(svc *background.ProjectPortabilityService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input background.ProjectImportInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.Import(ctx, input)
		if err != nil {
			if errors.Is(err, background.ErrConflict) {
				writeAPIError(c, consts.StatusConflict, 40921, err)
			} else {
				writeBackgroundError(c, err)
			}
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func DuplicateProject(svc *background.ProjectPortabilityService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		var input background.ProjectDuplicateInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.Duplicate(ctx, id, input)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func ResolveProjectRepositories(svc *background.ProjectPortabilityService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		result, err := svc.ResolveRepositories(ctx, id)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": result}})
	}
}
