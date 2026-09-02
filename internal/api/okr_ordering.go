package api

import (
	"context"
	"errors"
	"strings"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func ReorderObjectives(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input struct {
			Quarter      string   `json:"quarter"`
			ObjectiveIDs []string `json:"objective_ids"`
		}
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40047, err)
			return
		}
		order, err := service.ReorderObjectives(ctx, input.Quarter, input.ObjectiveIDs)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40447, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40047, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"order": order}})
	}
}

func ReorderKRs(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input struct {
			KRIDs []string `json:"kr_ids"`
		}
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40048, err)
			return
		}
		objectiveID := strings.TrimSpace(c.Param("objective_id"))
		order, err := service.ReorderKRs(ctx, objectiveID, input.KRIDs)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40448, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40048, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"order": order}})
	}
}
