package api

import (
	"context"
	"errors"

	"jarvis/internal/authn"
	"jarvis/internal/onboarding"
	"jarvis/internal/taskcreate"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type finalizeOnboardingRequest struct {
	AgentName string `json:"agent_name"`
	AppSecret string `json:"app_secret"`
}

func GetOnboardingStatus(service *onboarding.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		status, err := service.Status(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50040, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": status})
	}
}

func GetOnboardingBootstrap(service *onboarding.Service) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		status, err := service.Bootstrap()
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50040, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": status})
	}
}

func BeginOnboardingLarkSetup(service *onboarding.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		flow, err := service.BeginLarkSetup(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusBadGateway, 50240, err)
			return
		}
		c.JSON(consts.StatusAccepted, map[string]any{"code": 0, "data": flow})
	}
}

func BeginOnboardingLarkLogin(service *onboarding.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		flow, err := service.BeginLarkLogin(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusBadGateway, 50241, err)
			return
		}
		c.JSON(consts.StatusAccepted, map[string]any{"code": 0, "data": flow})
	}
}

func BeginOnboardingAgentLogin(service *onboarding.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		flow, err := service.BeginAgentLogin(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusBadGateway, 50242, err)
			return
		}
		c.JSON(consts.StatusAccepted, map[string]any{"code": 0, "data": flow})
	}
}

func GetOnboardingFlow(service *onboarding.Service) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		flow, err := service.Flow(c.Param("flow_id"))
		if err != nil {
			writeAPIError(c, consts.StatusNotFound, 40440, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": flow})
	}
}

func CancelOnboardingFlow(service *onboarding.Service) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		flow, err := service.CancelFlow(c.Param("flow_id"))
		if err != nil {
			writeAPIError(c, consts.StatusNotFound, 40440, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": flow})
	}
}

func FinalizeOnboarding(service *onboarding.Service, auth *authn.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input finalizeOnboardingRequest
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40041, err)
			return
		}
		user, ok := auth.Authenticate(string(c.Cookie(authn.CookieName)))
		if !ok {
			writeAPIError(c, consts.StatusUnauthorized, 40140, errors.New("字节身份登录已失效"))
			return
		}
		status, err := service.Finalize(ctx, input.AgentName, user.Email, input.AppSecret)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40042, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": status})
	}
}

func BootstrapOnboardingWorldModel(service *onboarding.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		task, err := service.BootstrapWorldModel(ctx)
		if err != nil {
			if errors.Is(err, taskcreate.ErrInvalidInput) {
				writeAPIError(c, consts.StatusBadRequest, 40043, err)
			} else {
				writeAPIError(c, consts.StatusInternalServerError, 50043, err)
			}
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": map[string]any{
			"task_id": task.ID,
			"status":  task.Status,
		}})
	}
}

// Credential repair updates the selected app without rerunning installation.
func RepairOnboardingLarkCredentials(service *onboarding.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input struct {
			AppSecret string `json:"app_secret"`
		}
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40041, err)
			return
		}
		if err := service.RepairLarkCredentials(ctx, input.AppSecret); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40042, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"saved": true}})
	}
}

func GetOnboardingPermissionConfig(service *onboarding.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		permissions, err := service.PermissionConfig(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50040, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": permissions})
	}
}
