package authn

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestBrowserMiddlewareRequiresSessionForBrowserAPI(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return nil, nil
	}})
	h := server.Default()
	h.Use(BrowserMiddleware(service))
	h.GET("/api/tasks", okHandler())

	request := ut.PerformRequest(h.Engine, "GET", "/api/tasks", nil,
		ut.Header{Key: "Sec-Fetch-Mode", Value: "cors"})
	if request.Result().StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", request.Result().StatusCode())
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

	h := server.Default()
	h.Use(BrowserMiddleware(service))
	h.GET("/api/tasks", okHandler())

	browser := ut.PerformRequest(h.Engine, "GET", "/api/tasks", nil,
		ut.Header{Key: "Sec-Fetch-Mode", Value: "cors"},
		ut.Header{Key: "Cookie", Value: CookieName + "=" + result.SessionToken})
	if browser.Result().StatusCode() != consts.StatusOK {
		t.Fatalf("authenticated browser status = %d", browser.Result().StatusCode())
	}
	localCLI := ut.PerformRequest(h.Engine, "GET", "/api/tasks", nil)
	if localCLI.Result().StatusCode() != consts.StatusOK {
		t.Fatalf("local CLI status = %d", localCLI.Result().StatusCode())
	}
}

func TestBrowserMiddlewareAllowsPublicClueEndpoint(t *testing.T) {
	service := newTestService(t, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return nil, nil
	}})
	h := server.Default()
	h.Use(BrowserMiddleware(service))
	h.POST("/api/clues", okHandler())

	response := ut.PerformRequest(h.Engine, "POST", "/api/clues", nil,
		ut.Header{Key: "Sec-Fetch-Mode", Value: "cors"})
	if response.Result().StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d, want 200", response.Result().StatusCode())
	}
}

func TestBrowserMiddlewareAllowsBrowserWhenAuthenticationDisabled(t *testing.T) {
	service, err := NewServiceWithRunner("bytedcli", time.Hour, false, fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		t.Fatal("disabled authentication must not invoke bytedcli")
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := server.Default()
	h.Use(BrowserMiddleware(service))
	h.GET("/api/tasks", okHandler())

	response := ut.PerformRequest(h.Engine, "GET", "/api/tasks", nil,
		ut.Header{Key: "Sec-Fetch-Mode", Value: "cors"})
	if response.Result().StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d, want 200", response.Result().StatusCode())
	}
}

func okHandler() app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		c.Status(consts.StatusOK)
	}
}
