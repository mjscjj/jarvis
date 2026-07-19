package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/background"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// --- Project handlers ---

func ListProjects(svc *background.ProjectService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter, err := backgroundListFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		result, err := svc.List(ctx, filter)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateProject(svc *background.ProjectService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in background.ProjectInput
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

func GetProject(svc *background.ProjectService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_id")
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

func UpdateProject(svc *background.ProjectService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		var in background.ProjectInput
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

func DeleteProject(svc *background.ProjectService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "project_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		if err := svc.Delete(ctx, id); err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id, "deleted": true}})
	}
}

// --- Person handlers ---

func ListPersons(svc *background.PersonService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter, err := backgroundListFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		result, err := svc.List(ctx, filter)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreatePerson(svc *background.PersonService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in background.PersonInput
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

func GetPerson(svc *background.PersonService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "person_id")
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

func UpdatePerson(svc *background.PersonService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "person_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		var in background.PersonInput
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

func DeletePerson(svc *background.PersonService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "person_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		if err := svc.Delete(ctx, id); err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id, "deleted": true}})
	}
}

// --- Group background handlers ---

func ListGroups(svc *background.GroupBackgroundService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		base, err := backgroundListFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		filter := background.GroupFilter{ListFilter: base}
		if raw := strings.TrimSpace(c.Query("related_only")); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("related_only must be true or false"))
				return
			}
			filter.RelatedOnly = value
		}
		result, err := svc.List(ctx, filter)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func UpdateGroupBackground(svc *background.GroupBackgroundService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "group_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		var in background.GroupBackgroundInput
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.UpdateBackground(ctx, id, in)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// --- shared helpers ---

func backgroundListFilter(c *app.RequestContext) (background.ListFilter, error) {
	page, err := positiveQueryInt(c.Query("page"), 1, "page")
	if err != nil {
		return background.ListFilter{}, err
	}
	pageSize, err := positiveQueryInt(c.Query("page_size"), 20, "page_size")
	if err != nil {
		return background.ListFilter{}, err
	}
	return background.ListFilter{Page: page, PageSize: pageSize}, nil
}

func backgroundID(c *app.RequestContext, name string) (uint64, error) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return id, nil
}

func writeBackgroundError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, background.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40023, err)
	case errors.Is(err, background.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40420, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50020, fmt.Errorf("background request failed: %s", strings.TrimSpace(err.Error())))
	}
}
