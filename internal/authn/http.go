package authn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	okrAuth "jarvis/internal/okrworkspace/auth"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const CookieName = "jarvis_session"

func (s *Service) AuthenticateRequest(ctx context.Context, c *app.RequestContext) (User, bool) {
	if s.okr != nil && s.okr.Enabled() && len(c.Cookie(s.okr.CookieName())) > 0 {
		session, err := s.okr.Current(ctx, string(c.Cookie(s.okr.CookieName())))
		if err == nil {
			if len(c.Cookie(s.CookieName())) > 0 {
				if err := s.clearBrowserSession(c); err != nil {
					hlog.CtxErrorf(ctx, "clear superseded browser session: %v", err)
					return User{}, false
				}
			}
			// The current OKR account takes precedence over a stale SSO cookie.
			user, mapped := s.okrAccountBindings[session.User.UnionID]
			return user, mapped && s.allows(user)
		}
		if !errors.Is(err, okrAuth.ErrUnauthenticated) {
			hlog.CtxErrorf(ctx, "read OKR browser identity: %v", err)
		}
		// An OKR cookie always owns the browser identity, including when it is
		// invalid or expired. Never revive a previous user's SSO session.
		return User{}, false
	}
	return s.Authenticate(string(c.Cookie(s.CookieName())))
}

func (s *Service) RequestStatus(ctx context.Context, c *app.RequestContext) View {
	if !s.enabled {
		return View{Enabled: false, Status: StatusUnauthenticated}
	}
	user, ok := s.AuthenticateRequest(ctx, c)
	// A verified OKR account may have a main-workbench preference while lacking
	// principal access here. Never turn that preference into an authenticated User.
	view := View{Enabled: true, Status: StatusUnauthenticated,
		PreferMainWorkbench: s.mainWorkbenchAccounts[strings.ToLower(user.Username)] || s.mainWorkbenchAccounts[strings.ToLower(user.Email)],
	}
	if ok {
		view.Status, view.User = StatusAuthenticated, &user
	}
	return view
}

func (s *Service) LogoutRequest(ctx context.Context, c *app.RequestContext) error {
	if err := s.clearBrowserSession(c); err != nil {
		return err
	}
	if s.okr != nil {
		if err := s.okr.Logout(ctx, string(c.Cookie(s.okr.CookieName()))); err != nil {
			return err
		}
		c.SetCookie(s.okr.CookieName(), "", -1, s.okr.CookiePath(), "", protocol.CookieSameSiteLaxMode, s.okr.CookieSecure(), true)
	}
	return nil
}

func (s *Service) clearBrowserSession(c *app.RequestContext) error {
	if err := s.Logout(string(c.Cookie(s.CookieName()))); err != nil {
		return err
	}
	c.SetCookie(s.CookieName(), "", -1, s.CookiePath(), "", protocol.CookieSameSiteStrictMode, false, true)
	return nil
}

// Only actual loopback CLI connections may skip browser identity verification.
func BrowserMiddleware(service *Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())
		if !service.Enabled() || !isProtectedBrowserPath(path) || isPublicPath(path, c.Method()) {
			c.Next(ctx)
			return
		}
		if _, ok := service.AuthenticateRequest(ctx, c); ok {
			c.Next(ctx)
			return
		}
		if !isBrowserRequest(c) && IsLoopbackRequest(c) {
			c.Next(ctx)
			return
		}
		c.AbortWithStatusJSON(consts.StatusUnauthorized, map[string]any{
			"code": 401,
			"msg":  "请先使用字节身份登录",
		})
	}
}

func isProtectedBrowserPath(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

// isPublicPath lists what a visitor reaches without a principal identity.
//
// App modules are open as a whole: each one runs its own visitor login and its
// own in-module access list, and the people who already use them are not the
// principal. `/okr-assets/` serves module images and is not matched here only
// because it is not under `/api/`. Everything else on this instance — tasks,
// the world model, prompts, settings, logs — belongs to the principal alone.
func isPublicPath(path string, method []byte) bool {
	for _, prefix := range []string{"/api/auth/", "/api/okr/", "/api/biz-okr/", "/api/okr-chat/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	switch path {
	// The single-page shell performs these reads before it can route anyone,
	// and the OKR comment/owner pickers resolve Feishu people through search.
	case "/api/agent-identity", "/api/web-config", "/api/people/search",
		"/api/setup/bootstrap", "/api/setup/status":
		return true
	case "/api/app-modules":
		return string(method) == consts.MethodGet
	}
	return false
}

func isBrowserRequest(c *app.RequestContext) bool {
	return len(c.Request.Header.Peek("Sec-Fetch-Mode")) > 0 ||
		len(c.Request.Header.Peek("Origin")) > 0
}

func IsLoopbackRequest(c *app.RequestContext) bool {
	return ClientIP(c).IsLoopback()
}

// ClientIP returns the effective client IP. X-Forwarded-For is trusted only
// from a loopback peer because the local production gateway overwrites that
// header before forwarding to Jarvis.
func ClientIP(c *app.RequestContext) net.IP {
	if c == nil {
		return nil
	}
	peerIP := addressIP(c.RemoteAddr())
	if peerIP == nil || !peerIP.IsLoopback() {
		return peerIP
	}
	forwarded := strings.TrimSpace(string(c.Request.Header.Peek("X-Forwarded-For")))
	if forwarded == "" || strings.Contains(forwarded, ",") {
		return peerIP
	}
	if forwardedIP := net.ParseIP(forwarded); forwardedIP != nil {
		return forwardedIP
	}
	return peerIP
}

func RedactIP(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return fmt.Sprintf("%d.%d.%d.*", ipv4[0], ipv4[1], ipv4[2])
	}
	ipv6 := ip.To16()
	if ipv6 == nil {
		return ""
	}
	return fmt.Sprintf(
		"%x:%x:%x:%x:*",
		uint16(ipv6[0])<<8|uint16(ipv6[1]),
		uint16(ipv6[2])<<8|uint16(ipv6[3]),
		uint16(ipv6[4])<<8|uint16(ipv6[5]),
		uint16(ipv6[6])<<8|uint16(ipv6[7]),
	)
}

func RedactAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		value = host
	}
	if ip := net.ParseIP(strings.Trim(value, "[]")); ip != nil {
		return RedactIP(ip)
	}
	if strings.HasSuffix(value, ".*") {
		return value
	}
	return ""
}

func addressIP(address net.Addr) net.IP {
	if address == nil {
		return nil
	}
	if strings.HasPrefix(address.Network(), "unix") {
		return net.IPv4(127, 0, 0, 1)
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(address.String()))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(address.String()), "[]")
	}
	return net.ParseIP(host)
}
