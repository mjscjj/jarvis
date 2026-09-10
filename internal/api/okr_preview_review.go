package api

import (
	"context"

	"jarvis/internal/okrreview"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// RunPreviewReview produces one advisory OKR Plan or progress review synchronously.
//
// The review writes nothing, so it creates no Task and leaves no run history:
// the caller gets Markdown back and shows it, or gets the failure. A review can
// take tens of seconds because the agent may look up related KRs.
func RunPreviewReview(service *okrreview.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request okrreview.Request
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40070, err)
			return
		}
		content, err := service.Review(ctx, request)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40071, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"content": content}})
	}
}
