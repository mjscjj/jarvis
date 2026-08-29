package api

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type AgentIdentityView struct {
	DisplayName string `json:"display_name"`
}

func GetAgentIdentity(displayName string) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{
			"code": 0,
			"data": AgentIdentityView{DisplayName: displayName},
		})
	}
}
