package api

import (
	"context"
	"errors"

	"jarvis/internal/authn"
	"jarvis/internal/observability"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type completeLoginRequest struct {
	FlowID string `json:"flow_id"`
}

func GetAuthStatus(service *authn.Service) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{
			"code": 0,
			"data": service.Status(string(c.Cookie(authn.CookieName))),
		})
	}
}

func LoginWithByteDance(service *authn.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		ctx = observability.FromRequestContext(ctx, c)
		result, err := service.Login(ctx)
		if err != nil {
			writeAuthError(c, err)
			return
		}
		setAuthCookie(c, service, result.SessionToken)
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result.View})
	}
}

func CompleteByteDanceLogin(service *authn.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		ctx = observability.FromRequestContext(ctx, c)
		var request completeLoginRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAuthError(c, err)
			return
		}
		result, err := service.Complete(ctx, request.FlowID)
		if errors.Is(err, authn.ErrPending) {
			c.JSON(consts.StatusOK, map[string]any{
				"code": 0,
				"data": authn.View{Enabled: true, Status: authn.StatusPending},
			})
			return
		}
		if errors.Is(err, authn.ErrNotAllowed) {
			c.JSON(consts.StatusForbidden, map[string]any{
				"code": 403,
				"msg":  "这个字节身份没有本实例的访问权限；OKR 模块请直接用飞书登录",
			})
			return
		}
		if err != nil {
			writeAuthError(c, err)
			return
		}
		setAuthCookie(c, service, result.SessionToken)
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result.View})
	}
}

func LogoutFromJarvis(service *authn.Service) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		service.Logout(string(c.Cookie(authn.CookieName)))
		c.SetCookie(authn.CookieName, "", -1, "/", "", protocol.CookieSameSiteStrictMode, false, true)
		c.JSON(consts.StatusOK, map[string]any{
			"code": 0,
			"data": service.Status(""),
		})
	}
}

func setAuthCookie(c *app.RequestContext, service *authn.Service, token string) {
	if token == "" {
		return
	}
	c.SetCookie(authn.CookieName, token, service.SessionMaxAge(), "/", "", protocol.CookieSameSiteStrictMode, false, true)
}

func writeAuthError(c *app.RequestContext, err error) {
	c.JSON(consts.StatusBadGateway, map[string]any{
		"code": 502,
		"msg":  err.Error(),
	})
}
