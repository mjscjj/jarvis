package api

import (
	"context"
	"errors"

	"jarvis/internal/agentconfig"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type AgentConfigService interface {
	Preview(ctx context.Context, stage string) (*agentconfig.Preview, error)
}

func GetAgentConfigPreview(service AgentConfigService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		preview, err := service.Preview(ctx, c.Param("agent_stage"))
		if err != nil {
			if errors.Is(err, agentconfig.ErrStageNotFound) {
				writeAPIError(c, consts.StatusNotFound, 40470, err)
				return
			}
			writeAPIError(c, consts.StatusInternalServerError, 50070, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": preview})
	}
}
