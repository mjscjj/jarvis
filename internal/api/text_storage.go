package api

import (
	"context"
	"errors"

	"jarvis/internal/textstore"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type TextFileService interface {
	List(ctx context.Context) ([]textstore.View, error)
	Get(ctx context.Context, key string) (*textstore.View, error)
	Update(ctx context.Context, key string, input textstore.Input) (*textstore.View, error)
}

func ListTextFiles(service TextFileService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		items, err := service.List(ctx)
		if err != nil {
			writeTextFileError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func GetTextFile(service TextFileService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		view, err := service.Get(ctx, c.Param("text_file_key"))
		if err != nil {
			writeTextFileError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func UpdateTextFile(service TextFileService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input textstore.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40050, err)
			return
		}
		view, err := service.Update(ctx, c.Param("text_file_key"), input)
		if err != nil {
			writeTextFileError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func writeTextFileError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, textstore.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40052, err)
	case errors.Is(err, textstore.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40450, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50050, err)
	}
}
