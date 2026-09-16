package api

import (
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"jarvis/internal/okrworkspace"
)

func RetryOKRCommentNotifications(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request struct {
			Email         string `json:"email"`
			ResendUnknown bool   `json:"resend_unknown"`
		}
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, 400, 40062, err)
			return
		}
		result, err := service.RetryCommentNotifications(ctx, c.Param("comment_id"), request.Email, request.ResendUnknown)
		if err != nil {
			writeAPIError(c, 400, 40062, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": result})
	}
}
