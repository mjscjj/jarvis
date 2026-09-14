package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
)

// DevelopmentProxy routes a configured installation prefix to a container-owned Unix
// socket. Path routing is intentionally not a browser-origin isolation boundary.
func newDevelopmentHTTPProxy(socket, prefix string) *httputil.ReverseProxy {
	stem := strings.TrimSuffix(prefix, "/")
	cookiePrefix := fmt.Sprintf("jarvis_%x_", sha256.Sum256([]byte(prefix)))
	return &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL.Scheme = "http"
			p.Out.URL.Host = "development"
			p.Out.URL.Path = strings.TrimPrefix(p.In.URL.Path, stem)
			p.Out.URL.RawPath = ""
			p.Out.Host = p.In.Host
			p.Out.Header.Del("Cookie")
			for _, cookie := range p.In.Cookies() {
				if strings.HasPrefix(cookie.Name, cookiePrefix) {
					cookie.Name = strings.TrimPrefix(cookie.Name, cookiePrefix)
					p.Out.AddCookie(cookie)
				}
			}
			// Browser requests forwarded over a local socket are not trusted local CLI.
			p.Out.Header.Set("X-Forwarded-For", "192.0.2.1")
			p.Out.Header.Set("X-Forwarded-Prefix", stem)
		},
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socket)
		}},
		FlushInterval: -1,
		ModifyResponse: func(r *http.Response) error {
			cookies := r.Cookies()
			r.Header.Del("Set-Cookie")
			for _, cookie := range cookies {
				cookie.Name = cookiePrefix + cookie.Name
				cookie.Path = prefix
				cookie.Domain = ""
				r.Header.Add("Set-Cookie", cookie.String())
			}
			if location := r.Header.Get("Location"); strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "//") && !strings.HasPrefix(location, prefix) {
				r.Header.Set("Location", stem+location)
			}
			return nil
		},
	}
}

func DevelopmentProxy(socket, prefix string) app.HandlerFunc {
	stem := strings.TrimSuffix(prefix, "/")
	handler := adaptor.HertzHandler(newDevelopmentHTTPProxy(socket, prefix))
	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())
		if path == stem {
			c.Redirect(http.StatusTemporaryRedirect, []byte(prefix))
			c.Abort()
			return
		}
		if strings.HasPrefix(path, prefix) {
			handler(ctx, c)
			c.Abort()
			return
		}
		c.Next(ctx)
	}
}
