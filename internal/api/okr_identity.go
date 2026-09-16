package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace"
	okrAuth "jarvis/internal/okrworkspace/auth"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const okrIdentityContextKey = "okr_identity"

var jarvisOKRUser = okrAuth.User{OpenID: "jarvis", Name: "Jarvis"}

type okrCurrentUserResponse struct {
	Authenticated           bool          `json:"authenticated"`
	Configured              bool          `json:"configured"`
	ManagementAccess        bool          `json:"management_access"`
	RegionalAutoMatchAccess bool          `json:"regional_auto_match_access"`
	ExpiresAt               *time.Time    `json:"expires_at,omitempty"`
	User                    *okrAuth.User `json:"user,omitempty"`
}

func GetOKRCurrentUser(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Enabled() {
			user := jarvisOKRUser
			c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrCurrentUserResponse{Authenticated: true, Configured: false, ManagementAccess: true, RegionalAutoMatchAccess: true, User: &user}})
			return
		}
		session, err := service.Current(ctx, string(c.Cookie(service.CookieName())))
		if errors.Is(err, okrAuth.ErrUnauthenticated) {
			c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrCurrentUserResponse{Configured: true}})
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50080, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrCurrentUserResponse{Authenticated: true, Configured: true, ManagementAccess: canManageOKR(session.User, true), RegionalAutoMatchAccess: canAutoMatchRegionalAlignment(session.User, true), ExpiresAt: &session.ExpiresAt, User: &session.User}})
	}
}

func BeginOKRFeishuDeviceLogin(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		login, err := service.BeginDeviceLogin(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50380, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": login})
	}
}

func PollOKRFeishuDeviceLogin(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		poll, token, err := service.PollDeviceLogin(ctx, c.Param("login_id"))
		if errors.Is(err, okrAuth.ErrDeviceLoginNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40480, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadGateway, 50280, err)
			return
		}
		if poll.Status == okrAuth.DeviceLoginCompleted {
			c.SetCookie(service.CookieName(), token, service.SessionMaxAge(), service.CookiePath(), "", protocol.CookieSameSiteLaxMode, service.CookieSecure(), true)
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": poll})
	}
}

func LogoutOKR(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		err := service.Logout(ctx, string(c.Cookie(service.CookieName())))
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50082, err)
			return
		}
		c.SetCookie(service.CookieName(), "", -1, service.CookiePath(), "", protocol.CookieSameSiteLaxMode, service.CookieSecure(), true)
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]bool{"logged_out": true}})
	}
}

func RequireOKRIdentity(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		user := jarvisOKRUser
		if service.Enabled() {
			session, err := service.Current(ctx, string(c.Cookie(service.CookieName())))
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
		return jarvisOKRUser
	}
	user, ok := value.(okrAuth.User)
	if !ok || strings.TrimSpace(user.OpenID) == "" {
		return jarvisOKRUser
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
