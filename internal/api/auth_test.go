package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"jarvis/internal/authn"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type authRunner struct {
	run func(string, []string) ([]byte, error)
}

func (r authRunner) Run(_ context.Context, bin string, args ...string) ([]byte, error) {
	return r.run(bin, args)
}

func TestLoginWithByteDanceStartsDeviceFlowWithoutSession(t *testing.T) {
	service, err := authn.NewServiceWithRunner("bytedcli", time.Hour, true, []string{"alice"}, authRunner{run: func(_ string, args []string) ([]byte, error) {
		if !strings.Contains(strings.Join(args, " "), "auth login --begin --session --session-method qr") {
			t.Fatalf("args = %v", args)
		}
		return []byte(`{"data":{"complete_token":"resume-1","verification_uri_complete":"https://sso.example/login"}}`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := authRequestContext("127.0.0.1", "")
	LoginWithByteDance(service)(context.Background(), request)
	if request.Response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", request.Response.StatusCode(), request.Response.Body())
	}
	if cookie := string(request.Response.Header.Peek("Set-Cookie")); cookie != "" {
		t.Fatalf("an unverified visitor received a session cookie: %q", cookie)
	}
}

// The host's own bytedcli identity belongs to the machine, not to whoever
// opened the page. Reusing it would sign every visitor in as the machine owner.
func TestRemoteLoginDoesNotReuseHostIdentity(t *testing.T) {
	service, err := authn.NewServiceWithRunner("bytedcli", time.Hour, true, []string{"alice"}, authRunner{run: func(_ string, args []string) ([]byte, error) {
		command := strings.Join(args, " ")
		if strings.Contains(command, "auth status") {
			return []byte(`{"data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
		}
		return []byte(`{"data":{"complete_token":"resume-1","verification_uri_complete":"https://sso.example/login"}}`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := authRequestContext("127.0.0.1", "10.20.30.40")

	LoginWithByteDance(service)(context.Background(), request)

	if request.Response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", request.Response.StatusCode(), request.Response.Body())
	}
	if cookie := string(request.Response.Header.Peek("Set-Cookie")); cookie != "" {
		t.Fatalf("a remote visitor inherited the host identity: %q", cookie)
	}
}

func TestCompleteByteDanceLoginRequiresFlowID(t *testing.T) {
	service, err := authn.NewServiceWithRunner("bytedcli", time.Hour, true, []string{"alice"}, authRunner{run: func(_ string, _ []string) ([]byte, error) {
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

func TestGetAuthStatusReportsDisabledBrowserGate(t *testing.T) {
	service, err := authn.NewServiceWithRunner("bytedcli", time.Hour, false, nil, authRunner{run: func(_ string, _ []string) ([]byte, error) {
		t.Fatal("disabled authentication must not invoke bytedcli")
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := server.Default()
	h.GET("/api/auth/status", GetAuthStatus(service))

	response := ut.PerformRequest(h.Engine, "GET", "/api/auth/status", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data authn.View `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Enabled || payload.Data.Status != authn.StatusUnauthenticated {
		t.Fatalf("view = %#v, want disabled", payload.Data)
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

func TestBrowserSessionCompletionCookie(t *testing.T) {
	for _, tc := range []struct {
		name, username string
		wantStatus     int
		wantCookie     bool
	}{
		{"allowed visitor", "alice", 200, true},
		{"other visitor", "mallory", 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, err := authn.NewServiceWithRunner("bytedcli", time.Hour, true, []string{"alice@bytedance.com"}, authRunner{run: func(_ string, args []string) ([]byte, error) {
				switch strings.Join(args[3:], " ") {
				case "auth login --begin --session --session-method qr":
					return []byte(`{"data":{"complete_token":"resume","verification_uri_complete":"https://sso.bytedance.com/qr"}}`), nil
				case "auth login --complete resume":
					return []byte(`{"status":"success","data":{"login_status":"success"}}`), nil
				case "auth userinfo":
					return json.Marshal(map[string]any{"data": map[string]string{"username": tc.username, "email": tc.username + "@bytedance.com"}})
				case "auth clear --yes":
					return []byte(`{"status":"success"}`), nil
				default:
					t.Fatalf("unexpected command: %v", args)
					return nil, nil
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			begin, err := service.Login(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			h := server.Default()
			h.POST("/api/auth/login/complete", CompleteByteDanceLogin(service))
			body, _ := json.Marshal(map[string]string{"flow_id": *begin.FlowID})
			response := ut.PerformRequest(h.Engine, "POST", "/api/auth/login/complete", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
			if response.StatusCode() != tc.wantStatus {
				t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
			}
			cookie := string(response.Header.Peek("Set-Cookie"))
			if (cookie != "") != tc.wantCookie {
				t.Fatalf("unexpected cookie: %q", cookie)
			}
			if tc.wantCookie && !strings.Contains(cookie, "HttpOnly") {
				t.Fatal("session cookie is not HttpOnly")
			}
		})
	}
}
