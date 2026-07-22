package api

import (
	"context"
	"errors"
	"strconv"

	"jarvis/internal/textstore"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type TextStorageService interface {
	List(ctx context.Context) ([]textstore.View, error)
	Create(ctx context.Context, input textstore.Input) (*textstore.View, error)
	Update(ctx context.Context, id uint64, input textstore.Input) (*textstore.View, error)
	Delete(ctx context.Context, id uint64) error
}

func ListTextStorage(service TextStorageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		items, err := service.List(ctx)
		if err != nil {
			writeTextStorageError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": items}})
	}
}

func CreateTextStorage(service TextStorageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input textstore.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40050, err)
			return
		}
		view, err := service.Create(ctx, input)
		if err != nil {
			writeTextStorageError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func UpdateTextStorage(service TextStorageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := textStorageID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40051, err)
			return
		}
		var input textstore.Input
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40050, err)
			return
		}
		view, err := service.Update(ctx, id, input)
		if err != nil {
			writeTextStorageError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func DeleteTextStorage(service TextStorageService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := textStorageID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40051, err)
			return
		}
		if err := service.Delete(ctx, id); err != nil {
			writeTextStorageError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": id, "deleted": true}})
	}
}

func textStorageID(c *app.RequestContext) (uint64, error) {
	return strconv.ParseUint(c.Param("text_storage_id"), 10, 64)
}

func writeTextStorageError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, textstore.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40052, err)
	case errors.Is(err, textstore.ErrNotFound):
		writeAPIError(c, consts.StatusNotFound, 40450, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50050, err)
	}
}
