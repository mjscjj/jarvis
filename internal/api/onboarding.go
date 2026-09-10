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

type bindLarkAppRequest struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

type finalizeOnboardingRequest struct {
	AgentName string `json:"agent_name"`
	AppID     string `json:"app_id"`
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

func BindOnboardingLarkApp(service *onboarding.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input bindLarkAppRequest
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40040, err)
			return
		}
		status, err := service.BindLarkApp(ctx, input.AppID, input.AppSecret)
		if err != nil {
			writeAPIError(c, consts.StatusBadGateway, 50240, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": status})
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
		status, err := service.Finalize(ctx, input.AgentName, user.Email, input.AppID, input.AppSecret)
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
