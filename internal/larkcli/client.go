// Package larkcli provides the single process boundary used for all lark-cli
// calls. It owns path resolution, rate/concurrency limits, timeout handling and
// the CLI response envelope contract.
package larkcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Options configures a Client. Invalid values are rejected at construction.
type Options struct {
	Bin         string
	Profile     string
	RateLimit   float64
	Burst       int
	Concurrency int
	Timeout     time.Duration
}

// Client is safe for concurrent use by all capture jobs.
type Client struct {
	bin     string
	profile string
	limiter *rate.Limiter
	sem     chan struct{}
	timeout time.Duration
}

// APIError is the structured error returned in a lark-cli {ok:false} envelope.
type APIError struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

func (e *APIError) Error() string {
	if e.Hint == "" {
		return fmt.Sprintf("lark-cli api error type=%s subtype=%s: %s", e.Type, e.Subtype, e.Message)
	}
	return fmt.Sprintf("lark-cli api error type=%s subtype=%s: %s (hint: %s)", e.Type, e.Subtype, e.Message, e.Hint)
}

// CommandError reports failures before a valid successful CLI envelope exists.
type CommandError struct {
	Args     []string
	ExitCode int
	Stderr   string
	Cause    error
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("lark-cli %q exit=%d: %v; stderr=%s", e.Args, e.ExitCode, e.Cause, e.Stderr)
}

func (e *CommandError) Unwrap() error { return e.Cause }

type envelope struct {
	OK    bool      `json:"ok"`
	Error *APIError `json:"error"`
}

// New resolves the binary eagerly so a broken deployment fails at startup.
func New(opts Options) (*Client, error) {
	if opts.Bin == "" {
		return nil, fmt.Errorf("lark-cli bin is empty")
	}
	if opts.RateLimit <= 0 {
		return nil, fmt.Errorf("lark-cli rate limit must be positive")
	}
	if opts.Burst <= 0 {
		return nil, fmt.Errorf("lark-cli burst must be positive")
	}
	if opts.Concurrency <= 0 {
		return nil, fmt.Errorf("lark-cli concurrency must be positive")
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("lark-cli timeout must be positive")
	}

	bin, err := exec.LookPath(opts.Bin)
	if err != nil {
		return nil, fmt.Errorf("resolve lark-cli binary %q: %w", opts.Bin, err)
	}
	return &Client{
		bin:     bin,
		profile: strings.TrimSpace(opts.Profile),
		limiter: rate.NewLimiter(rate.Limit(opts.RateLimit), opts.Burst),
		sem:     make(chan struct{}, opts.Concurrency),
		timeout: opts.Timeout,
	}, nil
}

// UserCandidate is one match returned by `contact +search-user`. Field names
// mirror the live lark-cli JSON (verified on this machine): the display name is
// localized_name, not name, and there is no en_name/avatar/title.
type UserCandidate struct {
	OpenID          string `json:"open_id"`
	LocalizedName   string `json:"localized_name"`
	Email           string `json:"email"`
	EnterpriseEmail string `json:"enterprise_email"`
	Department      string `json:"department"`
	P2PChatID       string `json:"p2p_chat_id"`
	IsCrossTenant   bool   `json:"is_cross_tenant"`
	HasChatted      bool   `json:"has_chatted"`
}

type searchUserResponse struct {
	Data struct {
		Users   []UserCandidate `json:"users"`
		HasMore bool            `json:"has_more"`
	} `json:"data"`
}

// SearchUser resolves a name/email query to candidate users via
// `contact +search-user --as user`. It returns the candidates plus the CLI's
// has_more flag verbatim; the caller decides how to surface an ambiguous match.
// fail-fast: an empty query is rejected and any CLI failure surfaces unchanged.
func (c *Client) SearchUser(ctx context.Context, query string) ([]UserCandidate, bool, error) {
	if strings.TrimSpace(query) == "" {
		return nil, false, fmt.Errorf("lark-cli search-user query is empty")
	}
	var resp searchUserResponse
	if err := c.Run(ctx, &resp, "contact", "+search-user", "--query", query, "--as", "user"); err != nil {
		return nil, false, fmt.Errorf("lark-cli search-user query=%q: %w", query, err)
	}
	return resp.Data.Users, resp.Data.HasMore, nil
}

// ChatMember is one human member of a chat as returned by
// `im +chat-members-list`. member_id is the person's open_id; bots are returned
// in a separate bucket and are intentionally not modeled here.
type ChatMember struct {
	MemberID  string `json:"member_id"`
	Name      string `json:"name"`
	TenantKey string `json:"tenant_key"`
}

type chatMembersResponse struct {
	Data struct {
		Users []ChatMember `json:"users"`
	} `json:"data"`
}

// ListChatMembers returns the human members of a chat, auto-paginating so the
// full roster is returned (the server caps a single page). Bots are excluded by
// only reading the users[] bucket. fail-fast: a blank chat id is rejected and
// any CLI failure surfaces unchanged.
func (c *Client) ListChatMembers(ctx context.Context, chatID string) ([]ChatMember, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, fmt.Errorf("lark-cli chat-members-list chat_id is empty")
	}
	var resp chatMembersResponse
	if err := c.Run(ctx, &resp, "im", "+chat-members-list", "--chat-id", chatID, "--member-types", "user", "--page-all", "--as", "user"); err != nil {
		return nil, fmt.Errorf("lark-cli chat-members-list chat_id=%q: %w", chatID, err)
	}
	return resp.Data.Users, nil
}

// RecallMessage recalls (deletes) one Feishu message via
// `im messages delete --as bot`. Identity is fixed to bot because Jarvis always
// sends as the bot, and only the sender can recall its own message.
//
// Recall is a lark-cli high-risk-write, so --yes must carry a human decision:
// the caller is the "撤回" button a human clicked on a specific message, which
// is that confirmation. fail-fast: a blank id is rejected and any CLI failure
// (already recalled, bot no longer in the chat, ...) surfaces unchanged.
func (c *Client) RecallMessage(ctx context.Context, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return fmt.Errorf("lark-cli messages delete message_id is empty")
	}
	var resp struct{}
	if err := c.Run(ctx, &resp, "im", "messages", "delete", "--message-id", messageID, "--as", "bot", "--yes"); err != nil {
		return fmt.Errorf("lark-cli messages delete message_id=%q: %w", messageID, err)
	}
	return nil
}

// Run executes a lark-cli command and unmarshals its successful JSON envelope.
// Callers must not pass --format; this boundary always forces JSON.
func (c *Client) Run(ctx context.Context, out any, args ...string) error {
	if c == nil {
		return fmt.Errorf("lark-cli client is nil")
	}
	if out == nil {
		return fmt.Errorf("lark-cli output target is nil")
	}
	for _, arg := range args {
		if arg == "--format" || strings.HasPrefix(arg, "--format=") || arg == "--json" {
			return fmt.Errorf("lark-cli output format is owned by the client")
		}
		if arg == "--profile" || strings.HasPrefix(arg, "--profile=") {
			return fmt.Errorf("lark-cli profile is owned by the client")
		}
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("wait for lark-cli rate limit: %w", err)
	}
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return fmt.Errorf("wait for lark-cli process slot: %w", ctx.Err())
	}

	commandCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	commandArgs := append([]string(nil), args...)
	if c.profile != "" {
		commandArgs = append(commandArgs, "--profile", c.profile)
	}
	commandArgs = append(commandArgs, "--format", "json")
	cmd := exec.CommandContext(commandCtx, c.bin, commandArgs...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// A few batch read shortcuts emit a useful structured response on
		// stdout and still exit non-zero when every item failed. Preserve that
		// payload for callers while keeping the process failure visible.
		if stdout.Len() > 0 {
			_ = json.Unmarshal(stdout.Bytes(), out)
		}
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		cause := err
		if commandCtx.Err() != nil {
			cause = commandCtx.Err()
		}
		return &CommandError{
			Args:     commandArgs,
			ExitCode: exitCode,
			Stderr:   strings.TrimSpace(stderr.String()),
			Cause:    cause,
		}
	}

	raw := stdout.Bytes()
	var meta envelope
	if err := json.Unmarshal(raw, &meta); err != nil {
		return fmt.Errorf("decode lark-cli envelope for %q: %w", commandArgs, err)
	}
	// Decode the typed payload before checking ok. Some read shortcuts return
	// ok=false with per-item errors in data (for example Minutes permission
	// denial) and no top-level error. Callers need that item-level payload to
	// classify and persist a retry state.
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode lark-cli response for %q: %w", commandArgs, err)
	}
	if !meta.OK {
		if meta.Error == nil {
			return fmt.Errorf("lark-cli %q returned ok=false without error", commandArgs)
		}
		return meta.Error
	}
	return nil
}
