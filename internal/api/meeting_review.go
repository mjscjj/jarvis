package api

import (
	"context"
	"errors"

	"jarvis/internal/insight"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func GetMeetingReviews(service *insight.MeetingReviewService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.Load(ctx, string(c.Query("date")))
		if errors.Is(err, insight.ErrInvalidReviewDate) {
			writeAPIError(c, consts.StatusBadRequest, 40045, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50045, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
