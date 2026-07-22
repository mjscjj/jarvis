package api

import (
	"context"
	"errors"
	"strconv"

	"jarvis/internal/workrule"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type WorkRuleService interface {
	List(ctx context.Context) ([]workrule.View, error)
	Create(ctx context.Context, input workrule.Input) (*workrule.View, error)
	Update(ctx context.Context, id uint64, input workrule.Input) (*workrule.View, error)
	Delete(ctx context.Context, id uint64) error
}

func ListWorkRules(service WorkRuleService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		items, err := service.List(ctx)
		if err != nil {
			writeWorkRuleError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func CreateWorkRule(service WorkRuleService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input workrule.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40040, err)
			return
		}
		view, err := service.Create(ctx, input)
		if err != nil {
			writeWorkRuleError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func UpdateWorkRule(service WorkRuleService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := workRuleID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40041, err)
			return
		}
		var input workrule.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40040, err)
			return
		}
		view, err := service.Update(ctx, id, input)
		if err != nil {
			writeWorkRuleError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func DeleteWorkRule(service WorkRuleService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := workRuleID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40041, err)
			return
		}
		if err := service.Delete(ctx, id); err != nil {
			writeWorkRuleError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id, "deleted": true}})
	}
}

func workRuleID(c *app.RequestContext) (uint64, error) {
	return strconv.ParseUint(c.Param("work_rule_id"), 10, 64)
}

func writeWorkRuleError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, workrule.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40042, err)
	case errors.Is(err, workrule.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40440, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50040, err)
	}
}
