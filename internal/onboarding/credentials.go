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

type larkConfig struct {
	AppID   string `json:"appId"`
	Brand   string `json:"brand"`
	Lang    string `json:"lang"`
	Profile string `json:"profile"`
}

func (s *Service) currentLarkConfig(ctx context.Context) (*larkConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := s.runner.Run(ctx, s.options.LarkCLIBin, []string{"config", "show"}, "")
	if err != nil {
		return nil, fmt.Errorf("无法读取当前飞书应用配置，请重新检查")
	}
	var current larkConfig
	if json.Unmarshal(output, &current) != nil || current.AppID == "" || current.Profile == "" {
		return nil, fmt.Errorf("当前飞书应用配置缺少 App ID 或 profile，请先完成连接")
	}
	return &current, nil
}

// RepairLarkCredentials updates the same app in its two existing consumers.
// It never changes principal configuration or initializes the world model.
func (s *Service) RepairLarkCredentials(ctx context.Context, secret string) error {
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	current, err := s.currentLarkConfig(ctx)
	if err != nil {
		return err
	}
	if current.Brand != "feishu" {
		return fmt.Errorf("当前密钥验证仅支持飞书应用")
	}
	secret = strings.TrimSpace(secret)
	if err := verifyAppCredentials(ctx, s.options.HTTPClient, current.AppID, secret); err != nil {
		return err
	}
	path := filepath.Join(s.options.StateRoot, "cc-connect", "config.toml")
	// Parse the existing config before updating either consumer, so malformed
	// or differently bound configs cannot be silently replaced.
	updated, err := updatedCCSecret(path, current.AppID, secret)
	if err != nil {
		return err
	}
	args := []string{"config", "init", "--name", current.Profile,
		"--app-id", current.AppID, "--brand", current.Brand, "--app-secret-stdin"}
	if current.Lang != "" {
		args = append(args, "--lang", current.Lang)
	}
	if _, err := s.runner.Run(ctx, s.options.LarkCLIBin, args, secret+"\n"); err != nil {
		return fmt.Errorf("更新当前飞书应用密钥失败，请重试；聊天配置尚未修改")
	}
	if updated == nil {
		return nil // First installation will create CC Connect config in Finalize.
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		return fmt.Errorf("飞书 CLI 已更新，但保存聊天配置失败，请重试：%w", err)
	}
	if err := os.WriteFile(filepath.Join(s.options.StateRoot, "restart.requested"), []byte("credentials\n"), 0o600); err != nil {
		return fmt.Errorf("密钥已保存，但请求重启失败，请重启 Jarvis：%w", err)
	}
	return nil
}

func updatedCCSecret(path, appID, secret string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法读取已有聊天配置：%w", err)
	}
	var document map[string]any
	if err := toml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("已有聊天配置格式错误，请先修复；不会覆盖现有配置")
	}
	projects, _ := document["projects"].([]any)
	for _, entry := range projects {
		project, _ := entry.(map[string]any)
		if project["name"] != "jarvis-codex" {
			continue
		}
		platforms, _ := project["platforms"].([]any)
		for _, entry := range platforms {
			platform, _ := entry.(map[string]any)
			options, _ := platform["options"].(map[string]any)
			if platform["type"] == "feishu" && options["app_id"] == appID {
				options["app_secret"] = secret
				return toml.Marshal(document)
			}
		}
	}
	return nil, fmt.Errorf("当前飞书应用与已有 Jarvis 聊天配置不匹配，请恢复原应用后重试；不会覆盖已有绑定")
}
