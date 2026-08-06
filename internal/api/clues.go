package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/capture"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type appendClueRequest struct {
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	OccurredAt string `json:"occurred_at"`
}

// AppendClue is the generic intake any agent uses to hand M2 an observed fact.
// M2 stores it as neutral evidence and wakes M3; deciding what the fact means
// belongs downstream, so this endpoint deliberately has no notion of kind,
// urgency or follow-up.
func AppendClue(svc *capture.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request appendClueRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, err)
			return
		}
		occurredAt, err := time.Parse(time.RFC3339, strings.TrimSpace(request.OccurredAt))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, fmt.Errorf("occurred_at must be RFC3339: %s", strings.TrimSpace(err.Error())))
			return
		}
		result, err := svc.AppendClue(ctx, capture.ClueInput{
			Source: request.Source, ExternalID: request.ExternalID,
			Title: request.Title, Content: request.Content, OccurredAt: occurredAt,
		})
		if errors.Is(err, capture.ErrInvalidClue) {
			writeAPIError(c, consts.StatusBadRequest, 40033, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50033, fmt.Errorf("append clue failed: %s", strings.TrimSpace(err.Error())))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
