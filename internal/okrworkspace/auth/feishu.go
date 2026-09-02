// Package auth implements browser identity without adding authorization rules.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const deviceIdentityScope = "offline_access"

var (
	ErrDeviceAuthorizationPending  = errors.New("Feishu device authorization is pending")
	ErrDeviceAuthorizationSlowDown = errors.New("Feishu device authorization polling is too frequent")
	ErrDeviceAuthorizationDenied   = errors.New("Feishu device authorization was denied")
	ErrDeviceAuthorizationExpired  = errors.New("Feishu device authorization expired")
)

type User struct {
	OpenID    string `json:"open_id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Email     string `json:"email,omitempty"`
}

type DeviceAuthorization struct {
	DeviceCode      string
	UserCode        string
	VerificationURL string
	ExpiresIn       time.Duration
	PollInterval    time.Duration
}

// Grant is one completed device authorization: who authorized, plus the tokens
// Feishu issued for them. The refresh token is why the device flow asks for the
// offline_access scope.
type Grant struct {
	User             User
	AccessToken      string
	RefreshToken     string
	TokenType        string
	Scope            string
	ExpiresIn        time.Duration
	RefreshExpiresIn time.Duration
}

type Provider interface {
	RequestDeviceAuthorization(ctx context.Context) (DeviceAuthorization, error)
	PollDeviceAuthorization(ctx context.Context, deviceCode string) (Grant, error)
}

// FeishuProvider uses Feishu's OAuth device flow. Unlike Web OAuth, this flow
// does not require a redirect URL.
type FeishuProvider struct {
	appID      string
	appSecret  string
	apiBaseURL string
	accountURL string
	httpClient *http.Client
}

func NewFeishuProvider(appID, appSecret, apiBaseURL, accountURL string, client *http.Client) (*FeishuProvider, error) {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	apiBaseURL = strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	accountURL = strings.TrimRight(strings.TrimSpace(accountURL), "/")
	if appID == "" || appSecret == "" || apiBaseURL == "" || accountURL == "" {
		return nil, fmt.Errorf("create Feishu provider: app id, app secret and base URLs are required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &FeishuProvider{appID: appID, appSecret: appSecret, apiBaseURL: apiBaseURL, accountURL: accountURL, httpClient: client}, nil
}

func (p *FeishuProvider) RequestDeviceAuthorization(ctx context.Context) (DeviceAuthorization, error) {
	form := url.Values{"client_id": {p.appID}, "scope": {deviceIdentityScope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.accountURL+"/oauth/v1/device_authorization", strings.NewReader(form.Encode()))
	if err != nil {
		return DeviceAuthorization{}, fmt.Errorf("create Feishu device authorization request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(p.appID, p.appSecret)
	var response struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
		Error                   string `json:"error"`
		ErrorDescription        string `json:"error_description"`
	}
	status, err := p.doJSON(req, &response)
	if err != nil {
		return DeviceAuthorization{}, fmt.Errorf("request Feishu device authorization: %w", err)
	}
	if strings.TrimSpace(response.Error) != "" {
		return DeviceAuthorization{}, fmt.Errorf("request Feishu device authorization: %s", oauthErrorMessage(response.Error, response.ErrorDescription))
	}
	if status < 200 || status >= 300 {
		return DeviceAuthorization{}, fmt.Errorf("request Feishu device authorization: unexpected HTTP status %d", status)
	}
	verificationURL := strings.TrimSpace(response.VerificationURIComplete)
	if verificationURL == "" {
		verificationURL = strings.TrimSpace(response.VerificationURI)
	}
	if strings.TrimSpace(response.DeviceCode) == "" || verificationURL == "" || response.ExpiresIn <= 0 || response.Interval <= 0 {
		return DeviceAuthorization{}, fmt.Errorf("request Feishu device authorization: response is missing device_code, verification URL, expires_in or interval")
	}
	return DeviceAuthorization{
		DeviceCode:      strings.TrimSpace(response.DeviceCode),
		UserCode:        strings.TrimSpace(response.UserCode),
		VerificationURL: verificationURL,
		ExpiresIn:       time.Duration(response.ExpiresIn) * time.Second,
		PollInterval:    time.Duration(response.Interval) * time.Second,
	}, nil
}

func (p *FeishuProvider) PollDeviceAuthorization(ctx context.Context, deviceCode string) (Grant, error) {
	deviceCode = strings.TrimSpace(deviceCode)
	if deviceCode == "" {
		return Grant{}, fmt.Errorf("poll Feishu device authorization: device code is required")
	}
	form := url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code":   {deviceCode},
		"client_id":     {p.appID},
		"client_secret": {p.appSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiBaseURL+"/open-apis/authen/v2/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Grant{}, fmt.Errorf("create Feishu device token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var response struct {
		AccessToken           string `json:"access_token"`
		RefreshToken          string `json:"refresh_token"`
		TokenType             string `json:"token_type"`
		Scope                 string `json:"scope"`
		ExpiresIn             int    `json:"expires_in"`
		RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
		Error                 string `json:"error"`
		ErrorDescription      string `json:"error_description"`
	}
	status, err := p.doJSON(req, &response)
	if err != nil {
		return Grant{}, fmt.Errorf("poll Feishu device authorization: %w", err)
	}
	switch strings.TrimSpace(response.Error) {
	case "authorization_pending":
		return Grant{}, ErrDeviceAuthorizationPending
	case "slow_down":
		return Grant{}, ErrDeviceAuthorizationSlowDown
	case "access_denied":
		return Grant{}, fmt.Errorf("%w: %s", ErrDeviceAuthorizationDenied, oauthErrorMessage(response.Error, response.ErrorDescription))
	case "expired_token", "invalid_grant":
		return Grant{}, fmt.Errorf("%w: %s", ErrDeviceAuthorizationExpired, oauthErrorMessage(response.Error, response.ErrorDescription))
	case "":
		// Continue below.
	default:
		return Grant{}, fmt.Errorf("poll Feishu device authorization: %s", oauthErrorMessage(response.Error, response.ErrorDescription))
	}
	if status < 200 || status >= 300 {
		return Grant{}, fmt.Errorf("poll Feishu device authorization: unexpected HTTP status %d", status)
	}
	accessToken := strings.TrimSpace(response.AccessToken)
	if accessToken == "" {
		return Grant{}, fmt.Errorf("poll Feishu device authorization: response has no access_token or error")
	}
	user, err := p.userInfo(ctx, accessToken)
	if err != nil {
		return Grant{}, err
	}
	return Grant{
		User:             user,
		AccessToken:      accessToken,
		RefreshToken:     strings.TrimSpace(response.RefreshToken),
		TokenType:        strings.TrimSpace(response.TokenType),
		Scope:            strings.TrimSpace(response.Scope),
		ExpiresIn:        time.Duration(response.ExpiresIn) * time.Second,
		RefreshExpiresIn: time.Duration(response.RefreshTokenExpiresIn) * time.Second,
	}, nil
}

func (p *FeishuProvider) userInfo(ctx context.Context, accessToken string) (User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiBaseURL+"/open-apis/authen/v1/user_info", nil)
	if err != nil {
		return User{}, fmt.Errorf("create Feishu user info request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OpenID    string `json:"open_id"`
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
			Email     string `json:"email"`
		} `json:"data"`
	}
	status, err := p.doJSON(req, &response)
	if err != nil {
		return User{}, fmt.Errorf("get Feishu user info: %w", err)
	}
	if status < 200 || status >= 300 || response.Code != 0 || strings.TrimSpace(response.Data.OpenID) == "" {
		return User{}, fmt.Errorf("get Feishu user info: code=%d msg=%s", response.Code, safeMessage(response.Msg))
	}
	name := strings.TrimSpace(response.Data.Name)
	if name == "" {
		name = "飞书用户"
	}
	return User{OpenID: strings.TrimSpace(response.Data.OpenID), Name: name, AvatarURL: strings.TrimSpace(response.Data.AvatarURL), Email: strings.TrimSpace(response.Data.Email)}, nil
}

func (p *FeishuProvider) doJSON(req *http.Request, target any) (int, error) {
	res, err := p.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return res.StatusCode, fmt.Errorf("read response: %w", err)
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(target); err != nil {
		return res.StatusCode, fmt.Errorf("decode HTTP %d response: %w", res.StatusCode, err)
	}
	return res.StatusCode, nil
}

func oauthErrorMessage(code, description string) string {
	description = strings.TrimSpace(description)
	if description != "" {
		return safeMessage(description)
	}
	return safeMessage(code)
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
