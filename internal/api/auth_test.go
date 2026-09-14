package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/authn"
	"jarvis/internal/domain"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type authRunner struct {
	run func(string, []string) ([]byte, error)
}

func (r authRunner) Run(_ context.Context, bin string, args ...string) ([]byte, error) {
	return r.run(bin, args)
}

func TestLoginWithByteDanceSetsJarvisSessionCookie(t *testing.T) {
	service, err := authn.NewServiceWithRunner(authTestDB(t), "bytedcli", time.Hour, authRunner{run: func(_ string, args []string) ([]byte, error) {
		if strings.Join(args, " ") != "--json auth status" {
			t.Fatalf("args = %v", args)
		}
		return []byte(`{"data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := authRequestContext("127.0.0.1", "")
	LoginWithByteDance(service)(context.Background(), request)
	if request.Response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", request.Response.StatusCode(), request.Response.Body())
	}
	if cookie := string(request.Response.Header.Peek("Set-Cookie")); !strings.Contains(cookie, authn.CookieName+"=") || !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("Set-Cookie = %q", cookie)
	}
}

func TestRemoteLoginReusesCurrentByteDanceIdentity(t *testing.T) {
	service, err := authn.NewServiceWithRunner(authTestDB(t), "bytedcli", time.Hour, authRunner{run: func(_ string, args []string) ([]byte, error) {
		if strings.Join(args, " ") != "--json auth status" {
			t.Fatalf("args = %v", args)
		}
		return []byte(`{"data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := authRequestContext("127.0.0.1", "10.20.30.40")

	LoginWithByteDance(service)(context.Background(), request)

	if request.Response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", request.Response.StatusCode(), request.Response.Body())
	}
	if cookie := string(request.Response.Header.Peek("Set-Cookie")); !strings.Contains(cookie, authn.CookieName+"=") || !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("Set-Cookie = %q", cookie)
	}
}

func TestCompleteByteDanceLoginRequiresFlowID(t *testing.T) {
	service, err := authn.NewServiceWithRunner(authTestDB(t), "bytedcli", time.Hour, authRunner{run: func(_ string, _ []string) ([]byte, error) {
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := server.Default()
	h.POST("/api/auth/login/complete", CompleteByteDanceLogin(service))
	body := []byte(`{}`)

	response := ut.PerformRequest(
		h.Engine, "POST", "/api/auth/login/complete",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
	).Result()
	if response.StatusCode() != consts.StatusBadGateway {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
}

type authRemoteConn struct {
	*mock.Conn
	remote net.Addr
}

func (c *authRemoteConn) RemoteAddr() net.Addr { return c.remote }

func authRequestContext(peer, forwarded string) *app.RequestContext {
	request := app.NewContext(0)
	request.SetConn(&authRemoteConn{
		Conn:   mock.NewConn(""),
		remote: &net.TCPAddr{IP: net.ParseIP(peer), Port: 18801},
	})
	request.Request.SetRequestURI("/api/auth/login")
	request.Request.Header.SetMethod("POST")
	if forwarded != "" {
		request.Request.Header.Set("X-Forwarded-For", forwarded)
	}
	return request
}

func authTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.BrowserSession{}); err != nil {
		t.Fatal(err)
	}
	return db
}
