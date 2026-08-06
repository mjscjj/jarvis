package api

import (
	"context"
	"errors"

	"jarvis/internal/workrule"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type WorkRuleService interface {
	List(ctx context.Context) ([]workrule.View, error)
	Get(ctx context.Context, key string) (*workrule.View, error)
	Update(ctx context.Context, key string, input workrule.Input) (*workrule.View, error)
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

func GetWorkRule(service WorkRuleService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		view, err := service.Get(ctx, c.Param("work_rule_key"))
		if err != nil {
			writeWorkRuleError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func UpdateWorkRule(service WorkRuleService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input workrule.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40040, err)
			return
		}
		view, err := service.Update(ctx, c.Param("work_rule_key"), input)
		if err != nil {
			writeWorkRuleError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
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
