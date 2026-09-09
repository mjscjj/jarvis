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
	Authenticated bool          `json:"authenticated"`
	Configured    bool          `json:"configured"`
	PlanAccess    okrPlanAccess `json:"plan_access"`
	ExpiresAt     *time.Time    `json:"expires_at,omitempty"`
	User          *okrAuth.User `json:"user,omitempty"`
}

func GetOKRCurrentUser(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !service.Enabled() {
			user := jarvisOKRUser
			c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrCurrentUserResponse{Authenticated: true, Configured: false, PlanAccess: okrPlanAccessEditor, User: &user}})
			return
		}
		session, err := service.Current(ctx, string(c.Cookie(okrAuth.CookieName)))
		if errors.Is(err, okrAuth.ErrUnauthenticated) {
			c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrCurrentUserResponse{Configured: true, PlanAccess: okrPlanAccessNone}})
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50080, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrCurrentUserResponse{Authenticated: true, Configured: true, PlanAccess: okrPlanAccessForUser(session.User, true), ExpiresAt: &session.ExpiresAt, User: &session.User}})
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
			c.SetCookie(okrAuth.CookieName, token, service.SessionMaxAge(), "/", "", protocol.CookieSameSiteLaxMode, service.CookieSecure(), true)
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": poll})
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

// GetOKRFeishuIdentity refreshes one person's Feishu token if needed and
// reports where it now lives. It deliberately returns the file path instead of
// the token: the chat sidecar only needs to hand a location to its Agent, and
// only this process holds the app secret required to refresh.
func GetOKRFeishuIdentity(tokens *okrAuth.UserTokens, store *okrAuth.TokenStore, appID string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		openID := strings.TrimSpace(c.Query("open_id"))
		if openID == "" {
			writeAPIError(c, consts.StatusBadRequest, 40081, fmt.Errorf("open_id is required"))
			return
		}
		stored, err := tokens.Ensure(ctx, openID)
		if err != nil {
			switch {
			case errors.Is(err, okrAuth.ErrNoUserToken), errors.Is(err, okrAuth.ErrUserTokenUnusable):
				writeAPIError(c, consts.StatusNotFound, 40481, err)
			default:
				writeAPIError(c, consts.StatusBadGateway, 50281, err)
			}
			return
		}
		path, err := store.Path(openID)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50084, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{
			"open_id":    stored.OpenID,
			"name":       stored.Name,
			"app_id":     appID,
			"token_path": path,
			"expires_at": stored.ExpiresAt,
		}})
	}
}

func RequireOKRIdentity(service *okrAuth.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		user := jarvisOKRUser
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
