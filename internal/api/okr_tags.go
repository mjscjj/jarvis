package api

import (
	"context"
	"errors"
	"strings"

	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func ReplaceKRTags(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.ReplaceKRTagsInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40023, err)
			return
		}
		id := strings.TrimSpace(c.Param("kr_id"))
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.ReplaceKRTags(ctx, id, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetCoreKR(ctx, id)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50023, currentErr)
				return
			}
			writeAPIConflict(c, 40923, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40423, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40023, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
