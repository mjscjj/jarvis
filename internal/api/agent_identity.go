package api

import (
	"context"

	"jarvis/internal/background"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type AgentIdentityView struct {
	DisplayName     string `json:"display_name"`
	PrincipalOpenID string `json:"principal_open_id"`
}

func GetAgentIdentity(displayName string, profile *background.ProfileService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		principal, err := profile.Get(ctx)
		if err != nil {
			writeBackgroundError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{
			"code": 0,
			"data": AgentIdentityView{DisplayName: displayName, PrincipalOpenID: principal.OpenID},
		})
	}
}
