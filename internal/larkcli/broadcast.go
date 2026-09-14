package larkcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
)

// BroadcastSender uses an explicitly configured application, independent of
// CLI defaults and principal user credentials.
type BroadcastSender struct {
	client           *Client
	broadcastProfile string
	appID            string
}

type larkProfile struct {
	Name  string `json:"name"`
	AppID string `json:"appId"`
}

type botIdentity struct {
	Status    string `json:"status"`
	Available bool   `json:"available"`
	Verified  bool   `json:"verified"`
	AppName   string `json:"appName"`
}

type profileAuthStatus struct {
	AppID      string `json:"appId"`
	Verified   bool   `json:"verified"`
	Identities struct {
		Bot  botIdentity  `json:"bot"`
		User UserIdentity `json:"user"`
	} `json:"identities"`
}

func NewBroadcastSender(ctx context.Context, client *Client, appID, profile string) (*BroadcastSender, error) {
	if client == nil || strings.TrimSpace(appID) == "" || strings.TrimSpace(profile) == "" {
		return nil, fmt.Errorf("notification application and explicit profile are required")
	}
	profiles, err := client.profiles(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := uniqueProfileForApp(profiles, appID, profile)
	if err != nil {
		return nil, err
	}
	if err := client.verifyBroadcastProfile(ctx, selected, appID); err != nil {
		return nil, err
	}
	return &BroadcastSender{client: client, broadcastProfile: selected, appID: appID}, nil
}

func (s *BroadcastSender) AppID() string   { return s.appID }
func (s *BroadcastSender) Profile() string { return s.broadcastProfile }

func (c *Client) profiles(ctx context.Context) ([]larkProfile, error) {
	raw, err := c.runRaw(ctx, "", nil, "profile", "list")
	if err != nil {
		return nil, fmt.Errorf("list lark-cli profiles: %w", err)
	}
	var profiles []larkProfile
	if err := json.Unmarshal(raw, &profiles); err != nil {
		return nil, fmt.Errorf("decode lark-cli profiles: %w", err)
	}
	return profiles, nil
}

func uniqueProfileForApp(profiles []larkProfile, appID, preferred string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		for _, profile := range profiles {
			if profile.Name == preferred {
				if profile.AppID != appID {
					return "", fmt.Errorf("profile %q uses app %q, want %q", preferred, profile.AppID, appID)
				}
				return preferred, nil
			}
		}
		return "", fmt.Errorf("profile %q does not exist", preferred)
	}
	matches := make([]string, 0, 1)
	for _, profile := range profiles {
		if profile.AppID == appID {
			matches = append(matches, profile.Name)
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("app %q has %d profiles, want exactly one", appID, len(matches))
	}
	return matches[0], nil
}

func (c *Client) profileAuthStatus(ctx context.Context, profile string) (profileAuthStatus, error) {
	raw, err := c.runRaw(ctx, "", nil, "--profile", profile, "auth", "status", "--verify")
	if err != nil {
		return profileAuthStatus{}, fmt.Errorf("verify profile %q: %w", profile, err)
	}
	var status profileAuthStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return profileAuthStatus{}, fmt.Errorf("decode profile %q auth status: %w", profile, err)
	}
	return status, nil
}

func (c *Client) verifyBroadcastProfile(ctx context.Context, profile, appID string) error {
	status, err := c.profileAuthStatus(ctx, profile)
	if err != nil {
		return err
	}
	bot := status.Identities.Bot
	if status.AppID != appID || bot.Status != "ready" || !bot.Available || !bot.Verified || !status.Verified {
		return fmt.Errorf("profile %q is not a verified notification Bot (%s)", profile, appID)
	}
	var scopes struct {
		Data struct {
			Scopes []struct {
				GrantStatus int    `json:"grant_status"`
				Name        string `json:"scope_name"`
				Type        string `json:"scope_type"`
			} `json:"scopes"`
		} `json:"data"`
	}
	if err := c.Run(ctx, &scopes, "--profile", profile, "api", "GET", "/open-apis/application/v6/scopes", "--as", "bot"); err != nil {
		return fmt.Errorf("read notification Bot scopes: %w", err)
	}
	granted := make(map[string]bool)
	for _, scope := range scopes.Data.Scopes {
		if scope.GrantStatus == 1 && scope.Type == "tenant" {
			granted[scope.Name] = true
		}
	}
	for _, required := range []string{"im:message:send_as_bot"} {
		if !granted[required] {
			return fmt.Errorf("notification Bot is missing tenant scope %q", required)
		}
	}
	params, _ := json.Marshal(map[string]any{
		"app_id": appID, "user_page_size": 1000, "department_page_size": 100,
	})
	var visibility struct{}
	if err := c.Run(ctx, &visibility, "--profile", profile, "api", "GET", "/open-apis/application/v2/app/visibility", "--params", string(params), "--as", "bot"); err != nil {
		return fmt.Errorf("read notification Bot visibility: %w", err)
	}
	return nil
}

// SendCardToEmail preserves the message ID even when readback fails. The
// caller must persist it and retry readback, never send another message blindly.
func (s *BroadcastSender) SendCardToEmail(ctx context.Context, email, authorEmail, card, idempotencyKey string) (string, error) {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || address.Name != "" {
		return "", fmt.Errorf("invalid notification email")
	}
	if err := validateCard2(card); err != nil {
		return "", err
	}
	if idempotencyKey == "" || len(idempotencyKey) > 50 {
		return "", fmt.Errorf("invalid notification idempotency key")
	}
	if email == strings.TrimSpace(authorEmail) {
		return "", nil
	}
	data, _ := json.Marshal(map[string]string{"receive_id": email, "msg_type": "interactive", "content": card, "uuid": idempotencyKey})
	params := `{"receive_id_type":"email"}`
	var sent struct {
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	if err := s.client.Run(ctx, &sent, "--profile", s.broadcastProfile, "api", "POST", "/open-apis/im/v1/messages", "--params", params, "--data", string(data), "--as", "bot"); err != nil {
		return "", err
	}
	id := strings.TrimSpace(sent.Data.MessageID)
	if !strings.HasPrefix(id, "om_") {
		return "", fmt.Errorf("notification response has no message ID")
	}
	return id, s.VerifyMessage(ctx, id)
}

func (s *BroadcastSender) VerifyMessage(ctx context.Context, id string) error {
	var result struct {
		Data struct {
			Messages []struct {
				MessageID string `json:"message_id"`
			} `json:"messages"`
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := s.client.Run(ctx, &result, "--profile", s.broadcastProfile, "im", "+messages-mget", "--message-ids", id, "--as", "bot"); err != nil {
		return err
	}
	if result.Data.Total != 1 || len(result.Data.Messages) != 1 || result.Data.Messages[0].MessageID != id {
		return fmt.Errorf("notification readback did not match %s", id)
	}
	return nil
}

func validateCard2(card string) error {
	if strings.TrimSpace(card) == "" {
		return fmt.Errorf("content is empty")
	}
	var envelope struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal([]byte(card), &envelope); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if envelope.Schema != "2.0" {
		return fmt.Errorf("schema is %q, want %q", envelope.Schema, "2.0")
	}
	return nil
}
