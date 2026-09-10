package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Verify the submitted pair, not a possibly cached lark-cli token. Never return
// the upstream response body: it can contain credentials or an access token.
func verifyAppCredentials(ctx context.Context, client *http.Client, appID, secret string) error {
	if !strings.HasPrefix(appID, "cli_") || len(appID) == len("cli_") {
		return fmt.Errorf("App ID 必须以 cli_ 开头，请从飞书应用的「凭证与基础信息」复制")
	}
	if secret == "" || strings.ContainsAny(secret, "\r\n") {
		return fmt.Errorf("请填写完整的 App Secret，不要包含换行")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, err := json.Marshal(map[string]string{"app_id": appID, "app_secret": secret})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("暂时无法连接飞书验证凭据，请检查网络后重试；尚未保存新凭据")
	}
	defer response.Body.Close()
	var result struct {
		Code  *int   `json:"code"`
		Token string `json:"tenant_access_token"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&result) != nil {
		return fmt.Errorf("飞书凭据验证服务返回异常，请稍后重试；尚未保存新凭据")
	}
	if result.Code == nil || *result.Code != 0 || result.Token == "" {
		return fmt.Errorf("App ID 与 App Secret 验证未通过，请确认来自同一个企业自建应用，密钥未被重置；尚未保存新凭据")
	}
	return nil
}

// Reuse the existing channel configuration, never a second credential store.
func (s *Service) savedSecret(appID string) (string, error) {
	if appID == "" {
		return "", nil
	}
	raw, err := os.ReadFile(filepath.Join(s.options.StateRoot, "cc-connect", "config.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("无法读取已有聊天配置")
	}
	var saved struct {
		Projects []struct {
			Name      string `toml:"name"`
			Platforms []struct {
				Type    string `toml:"type"`
				Options struct {
					AppID     string `toml:"app_id"`
					AppSecret string `toml:"app_secret"`
				} `toml:"options"`
			} `toml:"platforms"`
		} `toml:"projects"`
	}
	if err := toml.Unmarshal(raw, &saved); err != nil {
		return "", fmt.Errorf("已有聊天配置格式错误，请先修复；不会覆盖现有配置")
	}
	for _, project := range saved.Projects {
		if project.Name != "jarvis-codex" {
			continue
		}
		for _, platform := range project.Platforms {
			if platform.Type == "feishu" && platform.Options.AppID == appID && platform.Options.AppSecret != "replace-during-bind" {
				return platform.Options.AppSecret, nil
			}
		}
	}
	return "", nil
}
