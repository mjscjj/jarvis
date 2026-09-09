package authn

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const CookieName = "jarvis_session"

// BrowserMiddleware requires a Jarvis session for browser and remote API
// traffic. Only a connection that actually originates from loopback may use
// the credential-free local CLI path.
func BrowserMiddleware(service *Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())
		if !service.Enabled() || !isProtectedBrowserPath(path) || isPublicPath(path) {
			c.Next(ctx)
			return
		}
		if _, ok := service.Authenticate(string(c.Cookie(CookieName))); ok {
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
	return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/okr-assets/")
}

func isPublicPath(path string) bool {
	return strings.HasPrefix(path, "/api/auth/") ||
		path == "/api/agent-identity"
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
