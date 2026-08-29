// Package auth implements browser identity without adding authorization rules.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type User struct {
	OpenID    string `json:"open_id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Email     string `json:"email,omitempty"`
}

type Provider interface {
	AuthorizationURL(state string) string
	ExchangeCode(ctx context.Context, code string) (User, error)
}

// FeishuProvider uses the documented Web OAuth flow. User tokens exist only
// during ExchangeCode and are never persisted by Jarvis.
type FeishuProvider struct {
	appID       string
	appSecret   string
	redirectURL string
	apiBaseURL  string
	accountURL  string
	httpClient  *http.Client
}

func NewFeishuProvider(appID, appSecret, redirectURL, apiBaseURL, accountURL string, client *http.Client) (*FeishuProvider, error) {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	redirectURL = strings.TrimSpace(redirectURL)
	apiBaseURL = strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	accountURL = strings.TrimRight(strings.TrimSpace(accountURL), "/")
	if appID == "" || appSecret == "" || redirectURL == "" || apiBaseURL == "" || accountURL == "" {
		return nil, fmt.Errorf("create Feishu provider: app id, app secret, redirect URL and base URLs are required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &FeishuProvider{appID: appID, appSecret: appSecret, redirectURL: redirectURL, apiBaseURL: apiBaseURL, accountURL: accountURL, httpClient: client}, nil
}

func (p *FeishuProvider) AuthorizationURL(state string) string {
	values := url.Values{"app_id": {p.appID}, "redirect_uri": {p.redirectURL}, "state": {state}}
	return p.accountURL + "/open-apis/authen/v1/authorize?" + values.Encode()
}

func (p *FeishuProvider) ExchangeCode(ctx context.Context, code string) (User, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return User{}, fmt.Errorf("exchange Feishu code: code is required")
	}
	appToken, err := p.appAccessToken(ctx)
	if err != nil {
		return User{}, err
	}
	var tokenResponse struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := p.requestJSON(ctx, http.MethodPost, p.apiBaseURL+"/open-apis/authen/v1/access_token", map[string]string{
		"grant_type": "authorization_code", "code": code,
	}, appToken, &tokenResponse); err != nil {
		return User{}, fmt.Errorf("exchange Feishu user token: %w", err)
	}
	if tokenResponse.Code != 0 || strings.TrimSpace(tokenResponse.Data.AccessToken) == "" {
		return User{}, fmt.Errorf("exchange Feishu user token: code=%d msg=%s", tokenResponse.Code, safeMessage(tokenResponse.Msg))
	}

	var userResponse struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OpenID    string `json:"open_id"`
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
			Email     string `json:"email"`
		} `json:"data"`
	}
	if err := p.requestJSON(ctx, http.MethodGet, p.apiBaseURL+"/open-apis/authen/v1/user_info", nil, tokenResponse.Data.AccessToken, &userResponse); err != nil {
		return User{}, fmt.Errorf("get Feishu user info: %w", err)
	}
	if userResponse.Code != 0 || strings.TrimSpace(userResponse.Data.OpenID) == "" {
		return User{}, fmt.Errorf("get Feishu user info: code=%d msg=%s", userResponse.Code, safeMessage(userResponse.Msg))
	}
	name := strings.TrimSpace(userResponse.Data.Name)
	if name == "" {
		name = "飞书用户"
	}
	return User{OpenID: strings.TrimSpace(userResponse.Data.OpenID), Name: name, AvatarURL: strings.TrimSpace(userResponse.Data.AvatarURL), Email: strings.TrimSpace(userResponse.Data.Email)}, nil
}

func (p *FeishuProvider) appAccessToken(ctx context.Context) (string, error) {
	var response struct {
		Code           int    `json:"code"`
		Msg            string `json:"msg"`
		AppAccessToken string `json:"app_access_token"`
	}
	if err := p.requestJSON(ctx, http.MethodPost, p.apiBaseURL+"/open-apis/auth/v3/app_access_token/internal", map[string]string{
		"app_id": p.appID, "app_secret": p.appSecret,
	}, "", &response); err != nil {
		return "", fmt.Errorf("get Feishu app token: %w", err)
	}
	if response.Code != 0 || strings.TrimSpace(response.AppAccessToken) == "" {
		return "", fmt.Errorf("get Feishu app token: code=%d msg=%s", response.Code, safeMessage(response.Msg))
	}
	return response.AppAccessToken, nil
}

func (p *FeishuProvider) requestJSON(ctx context.Context, method, endpoint string, body any, bearer string, target any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return fmt.Errorf("unexpected HTTP status %d", res.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(res.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func safeMessage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown error"
	}
	if len(value) > 200 {
		return value[:200]
	}
	return value
}
