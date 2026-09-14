package authn

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestBrowserMiddlewareRequiresSessionForBrowserAPI(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return nil, nil
	}})

	request := middlewareRequest(service, "127.0.0.1:18801", map[string]string{
		"Sec-Fetch-Mode": "cors",
	})
	if request.Response.StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", request.Response.StatusCode())
	}
}

func TestBrowserMiddlewareAllowsWholeOKRModuleWithoutSSO(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return nil, nil
	}})
	for _, path := range []string{
		"/api/okr/board",
		"/api/biz-okr/board",
		"/okr-assets/image.png",
		"/api/people/search",
		"/api/app-modules",
	} {
		request := middlewareRequestForPath(service, "10.0.0.8:43000", path, map[string]string{
			"Sec-Fetch-Mode": "cors",
		})
		if request.Response.StatusCode() != consts.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, request.Response.StatusCode())
		}
	}
}

func TestBrowserMiddlewareAllowsAuthenticatedBrowserAndLocalCLI(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return nil, nil
	}})
	service.now = func() time.Time { return time.Unix(100, 0) }
	result, err := service.startSession(User{Username: "alice", Email: "alice@bytedance.com"})
	if err != nil {
		t.Fatal(err)
	}

	browser := middlewareRequest(service, "10.0.0.8:43000", map[string]string{
		"Sec-Fetch-Mode": "cors",
		"Cookie":         CookieName + "=" + result.SessionToken,
	})
	if browser.Response.StatusCode() != consts.StatusOK {
		t.Fatalf("authenticated browser status = %d", browser.Response.StatusCode())
	}
	localCLI := middlewareRequest(service, "127.0.0.1:43000", nil)
	if localCLI.Response.StatusCode() != consts.StatusOK {
		t.Fatalf("local CLI status = %d", localCLI.Response.StatusCode())
	}
	image := middlewareRequestForPath(service, "10.0.0.8:43000", "/okr-assets/image.png", map[string]string{
		"Sec-Fetch-Mode": "no-cors",
		"Cookie":         CookieName + "=" + result.SessionToken,
	})
	if image.Response.StatusCode() != consts.StatusOK {
		t.Fatalf("authenticated image status = %d", image.Response.StatusCode())
	}
}

func TestBrowserMiddlewareRequiresSessionForRemoteRequestsWithoutBrowserHeaders(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return nil, nil
	}})
	tests := []struct {
		name      string
		peer      string
		forwarded string
	}{
		{name: "trusted loopback proxy", peer: "127.0.0.1:43000", forwarded: "10.0.0.8"},
		{name: "remote direct connection", peer: "10.0.0.8:43000"},
		{name: "remote cannot forge forwarding header", peer: "10.0.0.8:43000", forwarded: "127.0.0.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers := map[string]string{}
			if test.forwarded != "" {
				headers["X-Forwarded-For"] = test.forwarded
			}
			response := middlewareRequest(service, test.peer, headers)
			if response.Response.StatusCode() != consts.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Response.StatusCode())
			}
		})
	}
}

func TestClientIPTrustsForwardingHeaderOnlyFromLoopback(t *testing.T) {
	tests := []struct {
		name      string
		peer      string
		forwarded string
		want      string
	}{
		{name: "local direct", peer: "127.0.0.1:18801", want: "127.0.0.1"},
		{name: "local proxy", peer: "127.0.0.1:18801", forwarded: "10.20.30.40", want: "10.20.30.40"},
		{name: "remote forged header", peer: "10.20.30.40:18801", forwarded: "127.0.0.1", want: "10.20.30.40"},
		{name: "ambiguous forwarded chain", peer: "127.0.0.1:18801", forwarded: "10.20.30.40, 127.0.0.1", want: "127.0.0.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := requestContext(test.peer, map[string]string{"X-Forwarded-For": test.forwarded})
			if got := ClientIP(request).String(); got != test.want {
				t.Fatalf("ClientIP() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRedactAddress(t *testing.T) {
	if got := RedactAddress("10.20.30.40:18800"); got != "10.20.30.*" {
		t.Fatalf("RedactAddress(ipv4) = %q", got)
	}
	if got := RedactAddress("[2001:db8:1:2::5]:18800"); got != "2001:db8:1:2:*" {
		t.Fatalf("RedactAddress(ipv6) = %q", got)
	}
}

func TestBrowserMiddlewareAllowsBrowserWhenAuthenticationDisabled(t *testing.T) {
	service, err := NewServiceWithRunner(openAuthTestDB(t), "bytedcli", time.Hour, false, nil, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		t.Fatal("disabled authentication must not invoke bytedcli")
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	response := middlewareRequest(service, "10.0.0.8:43000", map[string]string{
		"Sec-Fetch-Mode": "cors",
	})
	if response.Response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d, want 200", response.Response.StatusCode())
	}
}

type remoteConn struct {
	*mock.Conn
	remote net.Addr
}

func (c *remoteConn) RemoteAddr() net.Addr { return c.remote }

func requestContext(peer string, headers map[string]string) *app.RequestContext {
	return requestContextForPath(peer, "/api/tasks", headers)
}

func requestContextForPath(peer, path string, headers map[string]string) *app.RequestContext {
	host, port, _ := net.SplitHostPort(peer)
	address := &net.TCPAddr{IP: net.ParseIP(host)}
	fmtPort, _ := net.LookupPort("tcp", port)
	address.Port = fmtPort
	request := app.NewContext(0)
	request.SetConn(&remoteConn{Conn: mock.NewConn(""), remote: address})
	request.Request.SetRequestURI(path)
	request.Request.Header.SetMethod("GET")
	for key, value := range headers {
		if value != "" {
			request.Request.Header.Set(key, value)
		}
	}
	return request
}

func middlewareRequest(service *Service, peer string, headers map[string]string) *app.RequestContext {
	return middlewareRequestForPath(service, peer, "/api/tasks", headers)
}

func middlewareRequestForPath(service *Service, peer, path string, headers map[string]string) *app.RequestContext {
	request := requestContextForPath(peer, path, headers)
	request.SetHandlers(app.HandlersChain{okHandler()})
	BrowserMiddleware(service)(context.Background(), request)
	return request
}

func okHandler() app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		c.Status(consts.StatusOK)
	}
}
