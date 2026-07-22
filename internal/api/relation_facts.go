package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

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

type retractRelationFactRequest struct {
	By     string `json:"by"`
	Reason string `json:"reason"`
}

func RetractRelationFact(service knowledge.FactService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		factID, err := strconv.ParseUint(c.Param("fact_id"), 10, 64)
		if err != nil || factID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40061, fmt.Errorf("fact_id must be a positive integer"))
			return
		}
		var request retractRelationFactRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40061, err)
			return
		}
		result, err := service.Retract(ctx, knowledge.RetractInput{
			FactID: factID,
			By:     request.By,
			Reason: request.Reason,
		})
		if err != nil {
			writeRelationFactError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
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
	filter := knowledge.FactFilter{
		Predicate: strings.TrimSpace(c.Query("predicate")),
		Page:      page,
		PageSize:  pageSize,
	}
	if err := parseRelationEntityFilter(c.Query("subject_type"), c.Query("subject_id"), &filter.SubjectType, &filter.SubjectID, "subject"); err != nil {
		return knowledge.FactFilter{}, err
	}
	if err := parseRelationEntityFilter(c.Query("object_type"), c.Query("object_id"), &filter.ObjectType, &filter.ObjectID, "object"); err != nil {
		return knowledge.FactFilter{}, err
	}
	if raw := strings.TrimSpace(c.Query("include_inactive")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return knowledge.FactFilter{}, fmt.Errorf("include_inactive must be true or false")
		}
		filter.IncludeInactive = value
	}
	if raw := strings.TrimSpace(c.Query("as_of")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return knowledge.FactFilter{}, fmt.Errorf("as_of must be RFC3339: %w", err)
		}
		filter.AsOf = value
	}
	return filter, nil
}

func parseRelationEntityFilter(rawType, rawID string, entityType **knowledge.EntityType, entityID **uint64, name string) error {
	rawType = strings.TrimSpace(rawType)
	rawID = strings.TrimSpace(rawID)
	if rawType == "" && rawID == "" {
		return nil
	}
	if rawType == "" || rawID == "" {
		return fmt.Errorf("%s_type and %s_id must be provided together", name, name)
	}
	id, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil || id == 0 {
		return fmt.Errorf("%s_id must be a positive integer", name)
	}
	typeValue := knowledge.EntityType(rawType)
	*entityType = &typeValue
	*entityID = &id
	return nil
}

func writeRelationFactError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, knowledge.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40062, err)
	case errors.Is(err, knowledge.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40460, err)
	case errors.Is(err, knowledge.ErrNotActive):
		writeAPIError(c, consts.StatusConflict, 40960, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50060, err)
	}
}
