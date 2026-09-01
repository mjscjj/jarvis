package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFeishuProviderDeviceFlow(t *testing.T) {
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/v1/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		appID, secret, ok := r.BasicAuth()
		if !ok || appID != "cli_test" || secret != "secret" {
			t.Fatalf("unexpected basic auth: %q %q %v", appID, secret, ok)
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "cli_test" || r.Form.Get("scope") != "offline_access" {
			t.Fatalf("unexpected device authorization form: %#v, %v", r.Form, err)
		}
		_, _ = w.Write([]byte(`{"device_code":"device-1","user_code":"ABCD-1234","verification_uri":"https://accounts.example/verify","verification_uri_complete":"https://accounts.example/verify?code=ABCD-1234","expires_in":600,"interval":5}`))
	})
	mux.HandleFunc("/open-apis/authen/v2/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || r.Form.Get("device_code") != "device-1" || r.Form.Get("client_id") != "cli_test" || r.Form.Get("client_secret") != "secret" {
			t.Fatalf("unexpected device token form: %#v, %v", r.Form, err)
		}
		polls++
		if polls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"u-test","expires_in":7200}`))
	})
	mux.HandleFunc("/open-apis/authen/v1/user_info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer u-test" {
			t.Fatal("missing user access token")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "data": map[string]string{"open_id": "ou_1", "name": "Emily", "avatar_url": "https://img.example/a.png", "email": "emily@example.com"}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	provider, err := NewFeishuProvider("cli_test", "secret", server.URL, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := provider.RequestDeviceAuthorization(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if authorization.DeviceCode != "device-1" || authorization.UserCode != "ABCD-1234" || authorization.VerificationURL != "https://accounts.example/verify?code=ABCD-1234" {
		t.Fatalf("authorization = %+v", authorization)
	}
	if _, err := provider.PollDeviceAuthorization(context.Background(), authorization.DeviceCode); !errors.Is(err, ErrDeviceAuthorizationPending) {
		t.Fatalf("first poll error = %v", err)
	}
	user, err := provider.PollDeviceAuthorization(context.Background(), authorization.DeviceCode)
	if err != nil {
		t.Fatal(err)
	}
	if user.OpenID != "ou_1" || user.Name != "Emily" || user.Email != "emily@example.com" {
		t.Fatalf("user = %#v", user)
	}
}
