package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"jarvis/internal/config"
)

// ApplicationCheck describes an application-side prerequisite, not user OAuth.
type ApplicationCheck struct {
	Event string `json:"event"`
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}

func (s *Service) botEventChecks(ctx context.Context) []ApplicationCheck {
	events := []string{"im.message.receive_v1"}
	// Desktop onboarding enables card approval during finalization, so the
	// callback must already be available. Source installs only need it when the
	// feature is explicitly enabled in their local runtime configuration.
	if s.options.Desktop {
		events = append(events, "card.action.trigger")
	} else if cfg, err := config.Load(s.options.ConfigPath); err == nil && cfg.CardApproval.Enabled {
		events = append(events, "card.action.trigger")
	}
	checks := make([]ApplicationCheck, len(events))
	for i, event := range events {
		checks[i].Event = event
	}
	var workers sync.WaitGroup
	for i := range checks {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			check := &checks[i]
			output, err := s.runner.RunJSON(ctx, s.options.LarkCLIBin, []string{"event", "consume", check.Event, "--as", "bot", "--dry-run"}, "")
			if err != nil {
				check.Error = fmt.Sprintf("检查飞书应用事件 %s 失败：%v；原始响应：%s", check.Event, err, strings.TrimSpace(string(output)))
				return
			}
			var result struct {
				OK   bool `json:"ok"`
				Data struct {
					Decision struct {
						Status string `json:"status"`
					} `json:"decision"`
				} `json:"data"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				check.Error = fmt.Sprintf("无法解析事件 %s 的检查响应：%v；原始响应：%s", check.Event, err, strings.TrimSpace(string(output)))
				return
			}
			if !result.OK || result.Data.Decision.Status == "" {
				check.Error = fmt.Sprintf("事件 %s 的检查协议异常：%s", check.Event, strings.TrimSpace(string(output)))
				return
			}
			check.Ready = result.Data.Decision.Status == "ready"
			if !check.Ready {
				check.Error = fmt.Sprintf("事件 %s 尚未就绪，请按检查结果修复当前应用：%s", check.Event, strings.TrimSpace(string(output)))
			}
		}(i)
	}
	workers.Wait()
	return checks
}

func botEventsError(checks []ApplicationCheck) error {
	var failures []error
	for _, check := range checks {
		if !check.Ready {
			failures = append(failures, errors.New(check.Error))
		}
	}
	return errors.Join(failures...)
}

func (s *Service) checkBotEvents(ctx context.Context) error {
	return botEventsError(s.botEventChecks(ctx))
}

// PermissionConfig reuses the same script that requests and checks user scopes.
func (s *Service) PermissionConfig(ctx context.Context) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := s.larkAuthorization(ctx, "permissions")
	if err != nil {
		return nil, commandError("读取安装权限清单", output, err)
	}
	if !json.Valid(output) {
		return nil, fmt.Errorf("安装权限清单不是有效 JSON")
	}
	return json.RawMessage(output), nil
}
