package api

import (
	"context"
	"errors"

	"jarvis/internal/skill"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type SkillService interface {
	List(ctx context.Context) ([]skill.View, error)
	Scan(ctx context.Context) ([]skill.View, error)
	Update(ctx context.Context, name string, input skill.Input) (*skill.View, error)
	Content(ctx context.Context, name string) (*skill.ContentView, error)
}

func ListSkills(service SkillService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		items, err := service.List(ctx)
		if err != nil {
			writeSkillError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func ScanSkills(service SkillService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		items, err := service.Scan(ctx)
		if err != nil {
			writeSkillError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func UpdateSkill(service SkillService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input skill.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40051, err)
			return
		}
		view, err := service.Update(ctx, c.Param("skill_name"), input)
		if err != nil {
			writeSkillError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func GetSkillContent(service SkillService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		view, err := service.Content(ctx, c.Param("skill_name"))
		if err != nil {
			writeSkillError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func writeSkillError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, skill.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40052, err)
	case errors.Is(err, skill.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40450, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50050, err)
	}
}
