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

func ListPages(svc *background.PageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter, err := pageListFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		result, err := svc.ListPages(ctx, filter)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetPage(svc *background.PageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		pageType, id, err := pageIdentity(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		result, err := svc.GetPage(ctx, pageType, id)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func UpdatePage(svc *background.PageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		pageType, id, err := pageIdentity(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		var in background.UpdatePageInput
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := svc.UpdatePage(ctx, pageType, id, in)
		if err != nil {
			writePageError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func ApplyOKREvidence(svc *background.PageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in background.OKREvidenceInput
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40025, err)
			return
		}
		result, err := svc.ApplyOKREvidence(ctx, in)
		if err != nil {
			var conflict *background.OKREvidenceConflictError
			if errors.As(err, &conflict) {
				c.JSON(consts.StatusConflict, map[string]any{
					"code": 40925, "msg": err.Error(), "data": conflict.Result,
				})
				return
			}
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func ListUnassociatedOKREvidence(svc *background.PageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, err := positiveQueryInt(c.Query("limit"), 20, "limit")
		if err != nil || limit > 100 {
			if err == nil {
				err = fmt.Errorf("limit must not exceed 100")
			}
			writeAPIError(c, consts.StatusBadRequest, 40026, err)
			return
		}
		from, err := optionalRFC3339(c.Query("from"), "from")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40026, err)
			return
		}
		until, err := optionalRFC3339(c.Query("until"), "until")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40026, err)
			return
		}
		result, err := svc.ListUnassociatedOKREvidence(ctx, background.UnassociatedOKREvidenceFilter{
			Source: c.Query("source"), Anchor: c.Query("anchor"),
			From: from, Until: until, Limit: limit,
		})
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func ListPageBacklinks(svc *background.PageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		pageType, id, err := pageIdentity(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		result, err := svc.Backlinks(ctx, pageType, id)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func pageIdentity(c *app.RequestContext) (string, uint64, error) {
	pageType := strings.TrimSpace(c.Param("type"))
	if pageType == "" {
		return "", 0, fmt.Errorf("page type must not be blank")
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return "", 0, fmt.Errorf("id must be a positive integer")
	}
	return pageType, id, nil
}

func pageListFilter(c *app.RequestContext) (background.ListPagesFilter, error) {
	filter := background.ListPagesFilter{Type: strings.TrimSpace(c.Query("type"))}
	if raw := strings.TrimSpace(c.Query("all")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return background.ListPagesFilter{}, fmt.Errorf("all must be true or false")
		}
		filter.All = value
	}
	if raw := strings.TrimSpace(c.Query("stale_days")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days < 1 {
			return background.ListPagesFilter{}, fmt.Errorf("stale_days must be a positive integer")
		}
		filter.StaleDays = &days
	}
	if raw := strings.TrimSpace(c.Query("over_limit")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return background.ListPagesFilter{}, fmt.Errorf("over_limit must be true or false")
		}
		filter.OverLimit = value
	}
	return filter, nil
}

func writePageError(c *app.RequestContext, err error) {
	var conflict *background.PageConflictError
	if errors.As(err, &conflict) {
		c.JSON(consts.StatusConflict, map[string]any{
			"code": 40924, "msg": err.Error(), "data": conflict.Current,
		})
		return
	}
	writeBackgroundError(c, err)
}
