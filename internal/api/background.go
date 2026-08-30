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
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id, "archived": true}})
	}
}

// --- Key matter handlers ---

func ListKeyMatters(svc *background.KeyMatterService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		base, err := backgroundListFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		filter := background.KeyMatterFilter{ListFilter: base}
		if raw := strings.TrimSpace(c.Query("include_closed")); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("include_closed must be true or false"))
				return
			}
			filter.IncludeClosed = value
		}
		result, err := svc.List(ctx, filter)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateKeyMatter(svc *background.KeyMatterService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in background.KeyMatterInput
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

func GetKeyMatter(svc *background.KeyMatterService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "key_matter_id")
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

func UpdateKeyMatter(svc *background.KeyMatterService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "key_matter_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		var in background.KeyMatterInput
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

func TouchKeyMatter(svc *background.KeyMatterService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "key_matter_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		result, err := svc.Touch(ctx, id)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteKeyMatter(svc *background.KeyMatterService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "key_matter_id")
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
		var in background.PersonCreateInput
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

// ResolvePerson turns a name/email query into feishu open_id candidates via
// lark-cli so the person form never asks the user to type a raw ou_xxx id.
func ResolvePerson(svc *background.ResolveService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in struct {
			Query string `json:"query"`
		}
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.Resolve(ctx, in.Query)
		if err != nil {
			if errors.Is(err, background.ErrInvalidInput) {
				writeAPIError(c, consts.StatusBadRequest, 40023, err)
				return
			}
			// A resolve failure means the lark-cli upstream failed; surface it
			// as 502 rather than masking it as a generic server error.
			writeAPIError(c, consts.StatusBadGateway, 50210, fmt.Errorf("resolve person failed: %s", strings.TrimSpace(err.Error())))
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
		var in background.PersonUpdateInput
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
		filter := background.GroupFilter{
			ListFilter: base,
			Keyword:    strings.TrimSpace(c.Query("keyword")),
			ChatMode:   strings.TrimSpace(c.Query("chat_mode")),
			Tier:       strings.TrimSpace(c.Query("tier")),
		}
		if raw := strings.TrimSpace(c.Query("related_only")); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("related_only must be true or false"))
				return
			}
			filter.RelatedOnly = value
		}
		if raw := strings.TrimSpace(c.Query("key_only")); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("key_only must be true or false"))
				return
			}
			filter.KeyOnly = value
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

// --- Principal profile handlers ---

func GetProfile(svc *background.ProfileService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := svc.Get(ctx)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func UpdateProfile(svc *background.ProfileService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in background.ProfileInput
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.Upsert(ctx, in)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// --- Managed resource handlers ---

func ListResources(svc *background.ResourceService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter, err := resourceListFilter(c)
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

func CreateResource(svc *background.ResourceService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in background.ResourceInput
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

func GetResource(svc *background.ResourceService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "resource_id")
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

func UpdateResource(svc *background.ResourceService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "resource_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		var in background.ResourceInput
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

func TouchResource(svc *background.ResourceService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "resource_id")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		result, err := svc.Touch(ctx, id)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteResource(svc *background.ResourceService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := backgroundID(c, "resource_id")
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

// --- shared helpers ---

func resourceListFilter(c *app.RequestContext) (background.ResourceFilter, error) {
	base, err := backgroundListFilter(c)
	if err != nil {
		return background.ResourceFilter{}, err
	}
	filter := background.ResourceFilter{ListFilter: base}
	if raw := strings.TrimSpace(c.Query("person_id")); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			return background.ResourceFilter{}, fmt.Errorf("person_id must be a positive integer")
		}
		filter.PersonID = &id
	}
	if raw := strings.TrimSpace(c.Query("project_id")); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			return background.ResourceFilter{}, fmt.Errorf("project_id must be a positive integer")
		}
		filter.ProjectID = &id
	}
	if raw := strings.TrimSpace(c.Query("principal_only")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return background.ResourceFilter{}, fmt.Errorf("principal_only must be true or false")
		}
		filter.PrincipalOnly = value
	}
	if raw := strings.TrimSpace(c.Query("active_only")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return background.ResourceFilter{}, fmt.Errorf("active_only must be true or false")
		}
		filter.ActiveOnly = value
	}
	return filter, nil
}

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
	case errors.Is(err, background.ErrConflict):
		writeAPIError(c, consts.StatusConflict, 40922, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50020, fmt.Errorf("background request failed: %s", strings.TrimSpace(err.Error())))
	}
}
