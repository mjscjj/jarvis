package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/knowledge"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func CreateRelationFact(service knowledge.FactService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input knowledge.CreateInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40060, err)
			return
		}
		result, err := service.Create(ctx, input)
		if err != nil {
			writeRelationFactError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func ListRelationFacts(service knowledge.FactService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter, err := relationFactFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40060, err)
			return
		}
		result, err := service.List(ctx, filter)
		if err != nil {
			writeRelationFactError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func UpdateRelationFact(service knowledge.FactService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		factID, err := relationFactID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40061, err)
			return
		}
		var input knowledge.UpdateInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40061, err)
			return
		}
		input.FactID = factID
		result, err := service.Update(ctx, input)
		if err != nil {
			writeRelationFactError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteRelationFact(service knowledge.FactService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		factID, err := relationFactID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40061, err)
			return
		}
		if err := service.Delete(ctx, factID); err != nil {
			writeRelationFactError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": factID, "deleted": true}})
	}
}

func relationFactFilter(c *app.RequestContext) (knowledge.FactFilter, error) {
	page, err := positiveQueryInt(c.Query("page"), 1, "page")
	if err != nil {
		return knowledge.FactFilter{}, err
	}
	pageSize, err := positiveQueryInt(c.Query("page_size"), 20, "page_size")
	if err != nil {
		return knowledge.FactFilter{}, err
	}
	filter := knowledge.FactFilter{Page: page, PageSize: pageSize}
	rawType := strings.TrimSpace(c.Query("entity_type"))
	rawID := strings.TrimSpace(c.Query("entity_id"))
	if rawType == "" && rawID == "" {
		return filter, nil
	}
	if rawType == "" || rawID == "" {
		return knowledge.FactFilter{}, fmt.Errorf("entity_type and entity_id must be provided together")
	}
	id, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil || id == 0 {
		return knowledge.FactFilter{}, fmt.Errorf("entity_id must be a positive integer")
	}
	entityType := knowledge.EntityType(rawType)
	filter.EntityType = &entityType
	filter.EntityID = &id
	return filter, nil
}

func relationFactID(c *app.RequestContext) (uint64, error) {
	factID, err := strconv.ParseUint(c.Param("fact_id"), 10, 64)
	if err != nil || factID == 0 {
		return 0, fmt.Errorf("fact_id must be a positive integer")
	}
	return factID, nil
}

func writeRelationFactError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, knowledge.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40062, err)
	case errors.Is(err, knowledge.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40460, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50060, err)
	}
}
