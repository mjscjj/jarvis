package api

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"jarvis/internal/authn"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type authRunner struct {
	run func(string, []string) ([]byte, error)
}

func (r authRunner) Run(_ context.Context, bin string, args ...string) ([]byte, error) {
	return r.run(bin, args)
}

func TestLoginWithByteDanceSetsJarvisSessionCookie(t *testing.T) {
	service, err := authn.NewServiceWithRunner("bytedcli", time.Hour, authRunner{run: func(_ string, args []string) ([]byte, error) {
		if strings.Join(args, " ") != "--json auth status" {
			t.Fatalf("args = %v", args)
		}
		return []byte(`{"data":{"authenticated":true,"bytecloud_auth":{"identity":{"username":"alice","email":"alice@bytedance.com"}}}}`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := server.Default()
	h.POST("/api/auth/login", LoginWithByteDance(service))

	response := ut.PerformRequest(h.Engine, "POST", "/api/auth/login", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if cookie := string(response.Header.Peek("Set-Cookie")); !strings.Contains(cookie, authn.CookieName+"=") || !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("Set-Cookie = %q", cookie)
	}
}

func TestCompleteByteDanceLoginRequiresFlowID(t *testing.T) {
	service, err := authn.NewServiceWithRunner("bytedcli", time.Hour, authRunner{run: func(_ string, _ []string) ([]byte, error) {
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
