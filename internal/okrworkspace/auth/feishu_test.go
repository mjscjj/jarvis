package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFeishuProviderOAuthExchange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/app_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["app_id"] != "cli_test" || body["app_secret"] != "secret" {
			t.Fatalf("unexpected app credentials request: %#v", body)
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","app_access_token":"a-test"}`))
	})
	mux.HandleFunc("/open-apis/authen/v1/access_token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer a-test" {
			t.Fatal("missing app access token")
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"access_token":"u-test"}}`))
	})
	mux.HandleFunc("/open-apis/authen/v1/user_info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer u-test" {
			t.Fatal("missing user access token")
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"open_id":"ou_1","name":"Emily","avatar_url":"https://img.example/a.png","email":"emily@example.com"}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	provider, err := NewFeishuProvider("cli_test", "secret", "https://app.example/callback", server.URL, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	authorize, err := url.Parse(provider.AuthorizationURL("state-1"))
	if err != nil || authorize.Query().Get("app_id") != "cli_test" || authorize.Query().Get("state") != "state-1" {
		t.Fatalf("bad authorization URL: %s, %v", authorize, err)
	}
	user, err := provider.ExchangeCode(context.Background(), "code-1")
	if err != nil {
		t.Fatal(err)
	}
	if user.OpenID != "ou_1" || user.Name != "Emily" || user.Email != "emily@example.com" {
		t.Fatalf("unexpected user: %#v", user)
	}
}
