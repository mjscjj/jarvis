package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpFeishuIdentities resolves the signed-in user's Feishu credentials through
// the main Jarvis service. The chat sidecar cannot do this itself: refreshing a
// token needs the Feishu app secret, which only the main process holds.
type httpFeishuIdentities struct {
	endpoint string
	client   *http.Client
}

// NewHTTPFeishuIdentities points the resolver at the main service's address,
// for example "127.0.0.1:18800".
func NewHTTPFeishuIdentities(mainAddr string, client *http.Client) (FeishuIdentityResolver, error) {
	mainAddr = strings.TrimSpace(mainAddr)
	if mainAddr == "" {
		return nil, fmt.Errorf("create Feishu identity resolver: main service address is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &httpFeishuIdentities{endpoint: "http://" + mainAddr + "/api/biz-okr/feishu-identity", client: client}, nil
}

func (h *httpFeishuIdentities) Resolve(ctx context.Context, openID string) (FeishuIdentity, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return FeishuIdentity{}, fmt.Errorf("resolve Feishu identity: open_id is required")
	}
	target := h.endpoint + "?" + url.Values{"open_id": {openID}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return FeishuIdentity{}, fmt.Errorf("create Feishu identity request: %w", err)
	}
	res, err := h.client.Do(req)
	if err != nil {
		return FeishuIdentity{}, fmt.Errorf("request Feishu identity of %s: %w", openID, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return FeishuIdentity{}, fmt.Errorf("read Feishu identity response: %w", err)
	}
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OpenID    string `json:"open_id"`
			Name      string `json:"name"`
			AppID     string `json:"app_id"`
			TokenPath string `json:"token_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return FeishuIdentity{}, fmt.Errorf("decode Feishu identity response (HTTP %d): %w", res.StatusCode, err)
	}
	if res.StatusCode != http.StatusOK || payload.Code != 0 {
		message := strings.TrimSpace(payload.Msg)
		if message == "" {
			message = strings.TrimSpace(string(body))
		}
		return FeishuIdentity{}, fmt.Errorf("%s", message)
	}
	if strings.TrimSpace(payload.Data.TokenPath) == "" || strings.TrimSpace(payload.Data.AppID) == "" {
		return FeishuIdentity{}, fmt.Errorf("Feishu identity of %s has no token path or app id", openID)
	}
	return FeishuIdentity{
		OpenID:    payload.Data.OpenID,
		Name:      payload.Data.Name,
		AppID:     payload.Data.AppID,
		TokenPath: payload.Data.TokenPath,
	}, nil
}
