package larkcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	mainJarvisAppID     = "cli_a96a0c8d82b85cb1"
	mainJarvisBotName   = "Jarvis Bot"
	broadcastAppID      = "cli_a96a2422f03bdbd7"
	broadcastBotName    = "Jarvis通知机器人"
	sendAsBotScope      = "im:message:send_as_bot"
	sendMultiUsersScope = "im:message:send_multi_users"
)

// BroadcastSender is the only lark-cli boundary for notifications addressed
// to coworkers. It keeps identity normalization on the main Jarvis App and
// delivery on the dedicated notification App.
type BroadcastSender struct {
	client           *Client
	mainProfile      string
	broadcastProfile string
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

// NewBroadcastSender selects and verifies both App identities eagerly. A
// missing or ambiguous notification profile is fatal; delivery never falls
// back to the default Jarvis Bot.
func NewBroadcastSender(ctx context.Context, client *Client, preferredBroadcastProfile string) (*BroadcastSender, error) {
	if client == nil {
		return nil, fmt.Errorf("create Feishu broadcast sender: lark-cli client is nil")
	}
	profiles, err := client.profiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("create Feishu broadcast sender: %w", err)
	}
	mainProfile, err := uniqueProfileForApp(profiles, mainJarvisAppID, "")
	if err != nil {
		return nil, fmt.Errorf("create Feishu broadcast sender: main Jarvis profile: %w", err)
	}
	broadcastProfile, err := uniqueProfileForApp(profiles, broadcastAppID, preferredBroadcastProfile)
	if err != nil {
		return nil, fmt.Errorf("create Feishu broadcast sender: notification profile: %w", err)
	}
	if err := client.verifyMainProfile(ctx, mainProfile); err != nil {
		return nil, fmt.Errorf("create Feishu broadcast sender: %w", err)
	}
	if err := client.verifyBroadcastProfile(ctx, broadcastProfile); err != nil {
		return nil, fmt.Errorf("create Feishu broadcast sender: %w", err)
	}
	return &BroadcastSender{client: client, mainProfile: mainProfile, broadcastProfile: broadcastProfile}, nil
}

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

func (c *Client) verifyMainProfile(ctx context.Context, profile string) error {
	status, err := c.profileAuthStatus(ctx, profile)
	if err != nil {
		return err
	}
	if status.AppID != mainJarvisAppID || status.Identities.Bot.AppName != mainJarvisBotName {
		return fmt.Errorf("profile %q is not %s (%s)", profile, mainJarvisBotName, mainJarvisAppID)
	}
	user := status.Identities.User
	if !status.Verified || user.Status != "ready" || !user.Available || !user.Verified || user.TokenStatus != "valid" {
		return fmt.Errorf("profile %q has no verified principal user identity", profile)
	}
	return nil
}

func (c *Client) verifyBroadcastProfile(ctx context.Context, profile string) error {
	status, err := c.profileAuthStatus(ctx, profile)
	if err != nil {
		return err
	}
	bot := status.Identities.Bot
	if status.AppID != broadcastAppID || bot.AppName != broadcastBotName || bot.Status != "ready" || !bot.Available || !bot.Verified || !status.Verified {
		return fmt.Errorf("profile %q is not a verified %s Bot (%s)", profile, broadcastBotName, broadcastAppID)
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
	for _, required := range []string{sendAsBotScope, sendMultiUsersScope} {
		if !granted[required] {
			return fmt.Errorf("notification Bot is missing tenant scope %q", required)
		}
	}
	params, _ := json.Marshal(map[string]any{
		"app_id": broadcastAppID, "user_page_size": 1000, "department_page_size": 100,
	})
	var visibility struct{}
	if err := c.Run(ctx, &visibility, "--profile", profile, "api", "GET", "/open-apis/application/v2/app/visibility", "--params", string(params), "--as", "bot"); err != nil {
		return fmt.Errorf("read notification Bot visibility: %w", err)
	}
	return nil
}

// SendCardToMainAppUser resolves a main-App open_id to a stable enterprise
// email, skips the author by email, sends a Card 2.0 message with the
// notification Bot, and reads it back before reporting success.
func (s *BroadcastSender) SendCardToMainAppUser(ctx context.Context, recipientOpenID, recipientName, authorEmail, card, idempotencyKey string) error {
	recipientOpenID = strings.TrimSpace(recipientOpenID)
	recipientName = strings.TrimSpace(recipientName)
	card = strings.TrimSpace(card)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if recipientOpenID == "" || recipientName == "" {
		return fmt.Errorf("notification recipient open_id and name are required")
	}
	if err := validateCard2(card); err != nil {
		return fmt.Errorf("notification card is invalid: %w", err)
	}
	if idempotencyKey == "" || len(idempotencyKey) > 50 {
		return fmt.Errorf("notification idempotency key must contain 1 to 50 characters")
	}
	var users searchUserResponse
	if err := s.client.Run(ctx, &users, "--profile", s.mainProfile, "contact", "+search-user", "--user-ids", recipientOpenID, "--as", "user"); err != nil {
		return fmt.Errorf("resolve main-App user %q: %w", recipientOpenID, err)
	}
	if users.Data.HasMore || len(users.Data.Users) != 1 {
		return fmt.Errorf("resolve main-App user %q returned %d users (has_more=%t), want exactly one", recipientOpenID, len(users.Data.Users), users.Data.HasMore)
	}
	user := users.Data.Users[0]
	if user.OpenID != recipientOpenID || strings.TrimSpace(user.LocalizedName) != recipientName {
		return fmt.Errorf("resolved main-App user does not match recipient %q (%s)", recipientName, recipientOpenID)
	}
	email := strings.TrimSpace(user.EnterpriseEmail)
	if email == "" {
		email = strings.TrimSpace(user.Email)
	}
	if email == "" {
		return fmt.Errorf("resolved main-App user %q has no enterprise email", recipientOpenID)
	}
	if strings.EqualFold(email, strings.TrimSpace(authorEmail)) {
		return nil
	}
	data, _ := json.Marshal(map[string]string{
		"receive_id": email, "msg_type": "interactive", "content": card, "uuid": idempotencyKey,
	})
	params, _ := json.Marshal(map[string]string{"receive_id_type": "email"})
	var sent struct {
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	if err := s.client.Run(ctx, &sent, "--profile", s.broadcastProfile, "api", "POST", "/open-apis/im/v1/messages", "--params", string(params), "--data", string(data), "--as", "bot"); err != nil {
		return fmt.Errorf("send comment notification to %q: %w", email, err)
	}
	messageID := strings.TrimSpace(sent.Data.MessageID)
	if !strings.HasPrefix(messageID, "om_") {
		return fmt.Errorf("send comment notification to %q returned invalid message_id %q", email, messageID)
	}
	var readBack struct {
		Data struct {
			Messages []struct {
				MessageID string `json:"message_id"`
			} `json:"messages"`
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := s.client.Run(ctx, &readBack, "--profile", s.broadcastProfile, "im", "+messages-mget", "--message-ids", messageID, "--as", "bot"); err != nil {
		return fmt.Errorf("read back comment notification %q: %w", messageID, err)
	}
	if readBack.Data.Total != 1 || len(readBack.Data.Messages) != 1 || readBack.Data.Messages[0].MessageID != messageID {
		return fmt.Errorf("read back comment notification %q returned an unexpected message set", messageID)
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
