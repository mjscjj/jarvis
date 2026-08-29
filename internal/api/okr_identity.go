package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/okrworkspace"
	okrAuth "jarvis/internal/okrworkspace/auth"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const okrIdentityContextKey = "okr_identity"

var localOKRUser = okrAuth.User{OpenID: "local", Name: "本机用户"}

type okrCurrentUserResponse struct {
	Authenticated bool          `json:"authenticated"`
	Configured    bool          `json:"configured"`
	User          *okrAuth.User `json:"user,omitempty"`
}

func GetOKRCurrentUser(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Enabled() {
			user := localOKRUser
			c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrCurrentUserResponse{Authenticated: true, Configured: false, User: &user}})
			return
		}
		session, err := service.Current(ctx, string(c.Cookie(okrAuth.CookieName)))
		if errors.Is(err, okrAuth.ErrUnauthenticated) {
			c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrCurrentUserResponse{Configured: true}})
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50080, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrCurrentUserResponse{Authenticated: true, Configured: true, User: &session.User}})
	}
}

func BeginOKRFeishuLogin(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		location, err := service.BeginLogin(ctx, strings.TrimSpace(c.Query("return_to")))
		if err != nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50380, err)
			return
		}
		c.Redirect(consts.StatusFound, []byte(location))
	}
}

func CompleteOKRFeishuLogin(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if oauthError := strings.TrimSpace(c.Query("error")); oauthError != "" {
			writeAPIError(c, consts.StatusBadRequest, 40080, fmt.Errorf("Feishu login was not completed: %s", oauthError))
			return
		}
		_, token, returnTo, err := service.CompleteLogin(ctx, strings.TrimSpace(c.Query("code")), strings.TrimSpace(c.Query("state")))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40081, err)
			return
		}
		c.SetCookie(okrAuth.CookieName, token, service.SessionMaxAge(), "/", "", protocol.CookieSameSiteLaxMode, service.CookieSecure(), true)
		c.Redirect(consts.StatusFound, []byte(returnTo))
	}
}

func LogoutOKR(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if err := service.Logout(ctx, string(c.Cookie(okrAuth.CookieName))); err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50082, err)
			return
		}
		c.SetCookie(okrAuth.CookieName, "", -1, "/", "", protocol.CookieSameSiteLaxMode, service.CookieSecure(), true)
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]bool{"logged_out": true}})
	}
}

func RequireOKRIdentity(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		user := localOKRUser
		if service.Enabled() {
			session, err := service.Current(ctx, string(c.Cookie(okrAuth.CookieName)))
			if errors.Is(err, okrAuth.ErrUnauthenticated) {
				writeAPIError(c, consts.StatusUnauthorized, 40180, fmt.Errorf("请先使用飞书登录"))
				c.Abort()
				return
			}
			if err != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50083, err)
				c.Abort()
				return
			}
			user = session.User
		}
		c.Set(okrIdentityContextKey, user)
		c.Next(ctx)
	}
}

func currentOKRIdentity(c *app.RequestContext) okrAuth.User {
	value, ok := c.Get(okrIdentityContextKey)
	if !ok {
		return localOKRUser
	}
	user, ok := value.(okrAuth.User)
	if !ok || strings.TrimSpace(user.OpenID) == "" {
		return localOKRUser
	}
	return user
}

func UploadOKRImage(store *okrworkspace.ImageStore) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		header, err := c.FormFile("image")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40083, fmt.Errorf("image multipart field is required: %w", err))
			return
		}
		file, err := header.Open()
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40084, fmt.Errorf("open uploaded image: %w", err))
			return
		}
		defer file.Close()
		result, err := store.Save(header.Filename, file)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40085, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}
