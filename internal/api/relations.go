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

func ListRelations(service *background.RelationService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit := 100
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40091, err)
				return
			}
			limit = parsed
		}
		rows, err := service.List(ctx, background.RelationFilter{
			SourceType: c.Query("source_type"), SourceID: c.Query("source_id"),
			TargetType: c.Query("target_type"), TargetID: c.Query("target_id"), Limit: limit,
		})
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": rows}})
	}
}

func UpsertRelation(service *background.RelationService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input background.RelationInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40092, err)
			return
		}
		row, err := service.Upsert(ctx, input)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": row})
	}
}

func DeleteRelation(service *background.RelationService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := strconv.ParseUint(strings.TrimSpace(c.Param("relation_id")), 10, 64)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40093, err)
			return
		}
		if id == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40093, fmt.Errorf("relation_id must be positive"))
			return
		}
		if err := service.Delete(ctx, id); err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id}})
	}
}
