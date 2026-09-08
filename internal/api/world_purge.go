package api

import (
	"context"

	"jarvis/internal/background"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func PurgeWorldEntities(service *background.WorldPurgeService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input background.WorldPurgeInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40094, err)
			return
		}
		result, err := service.Purge(ctx, input)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
