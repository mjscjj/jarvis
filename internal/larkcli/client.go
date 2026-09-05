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
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Options configures a Client. Invalid values are rejected at construction.
type Options struct {
	Bin         string
	RateLimit   float64
	Burst       int
	Concurrency int
	Timeout     time.Duration
	// ExportSecureLabel 是导出文档时打上的密级标签名，取值来自租户策略。
	// 只有 CreateMarkdownDocument 用它，其它调用留空即可。
	ExportSecureLabel string
	// Timezone fixes the process timezone used by lark-cli when it renders
	// timestamps without an offset. Callers parsing those timestamps must use
	// the same location; inheriting the host timezone makes persisted epochs
	// change when the same Jarvis installation moves between machines.
	Timezone string
}

// Client is safe for concurrent use by all capture jobs.
type Client struct {
	bin               string
	limiter           *rate.Limiter
	sem               chan struct{}
	timeout           time.Duration
	exportSecureLabel string
	timezone          string
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
	if strings.TrimSpace(opts.Timezone) == "" {
		return nil, fmt.Errorf("lark-cli timezone is empty")
	}
	if _, err := time.LoadLocation(opts.Timezone); err != nil {
		return nil, fmt.Errorf("load lark-cli timezone %q: %w", opts.Timezone, err)
	}

	bin, err := exec.LookPath(opts.Bin)
	if err != nil {
		return nil, fmt.Errorf("resolve lark-cli binary %q: %w", opts.Bin, err)
	}
	return &Client{
		bin:               bin,
		limiter:           rate.NewLimiter(rate.Limit(opts.RateLimit), opts.Burst),
		sem:               make(chan struct{}, opts.Concurrency),
		timeout:           opts.Timeout,
		exportSecureLabel: strings.TrimSpace(opts.ExportSecureLabel),
		timezone:          opts.Timezone,
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

// UserAvatar is one match returned by the legacy people search endpoint
// `/open-apis/search/v1/user`. It is the only people lookup this app's granted
// user scopes cover that carries avatar images; `contact +search-user` and
// `contact +get-user` return none, and `contact/v3/users` needs a scope the
// current authorization does not include.
type UserAvatar struct {
	OpenID string `json:"open_id"`
	Name   string `json:"name"`
	Avatar struct {
		Small  string `json:"avatar_72"`
		Medium string `json:"avatar_240"`
	} `json:"avatar"`
}

type searchUserAvatarResponse struct {
	Data struct {
		Users []UserAvatar `json:"users"`
	} `json:"data"`
}

// SearchUserAvatars looks a name or email up and returns the matches with their
// avatar URLs. The endpoint does not accept an open_id as the query, so callers
// search by name and pick the match by open_id themselves.
// fail-fast: an empty query is rejected and any CLI failure surfaces unchanged.
func (c *Client) SearchUserAvatars(ctx context.Context, query string) ([]UserAvatar, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("lark-cli avatar search query is empty")
	}
	params, err := json.Marshal(map[string]any{"query": query, "page_size": 20})
	if err != nil {
		return nil, fmt.Errorf("lark-cli avatar search params query=%q: %w", query, err)
	}
	var resp searchUserAvatarResponse
	if err := c.Run(ctx, &resp, "api", "GET", "/open-apis/search/v1/user", "--params", string(params), "--as", "user"); err != nil {
		return nil, fmt.Errorf("lark-cli avatar search query=%q: %w", query, err)
	}
	return resp.Data.Users, nil
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
	return c.run(ctx, out, "", args...)
}

// RunInput is Run with explicit stdin. It is used by shortcuts such as
// `docs +create --content -`, keeping long user-authored content out of argv
// and avoiding shell escaping entirely.
func (c *Client) RunInput(ctx context.Context, out any, input string, args ...string) error {
	return c.run(ctx, out, input, args...)
}

func (c *Client) run(ctx context.Context, out any, input string, args ...string) error {
	if out == nil {
		return fmt.Errorf("lark-cli output target is nil")
	}
	raw, err := c.runRaw(ctx, input, []string{"--format", "json"}, args...)
	if err != nil {
		// A few batch read shortcuts emit a useful structured response on
		// stdout and still exit non-zero when every item failed. Preserve that
		// payload for callers while keeping the process failure visible.
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, out)
		}
		return err
	}

	var meta envelope
	if err := json.Unmarshal(raw, &meta); err != nil {
		return fmt.Errorf("decode lark-cli envelope for %q: %w", args, err)
	}
	// Decode the typed payload before checking ok. Some read shortcuts return
	// ok=false with per-item errors in data (for example Minutes permission
	// denial) and no top-level error. Callers need that item-level payload to
	// classify and persist a retry state.
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode lark-cli response for %q: %w", args, err)
	}
	if !meta.OK {
		if meta.Error == nil {
			return fmt.Errorf("lark-cli %q returned ok=false without error", args)
		}
		return meta.Error
	}
	return nil
}

// runRaw executes lark-cli and returns stdout uninterpreted. Only commands that
// do not speak the {ok:...} envelope call it directly (auth status is local CLI
// state, not an OpenAPI response); everything else goes through run so the
// envelope contract stays in one place. stdout is returned alongside a failure
// because some shortcuts write a usable payload and still exit non-zero.
//
// formatArgs carries how this command is asked for JSON, because that is not
// uniform across the CLI: API shortcuts take `--format json` while auth status
// emits JSON natively and rejects the flag. It stays a caller argument rather
// than a caller-supplied flag so args itself remains format-free.
func (c *Client) runRaw(ctx context.Context, input string, formatArgs []string, args ...string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("lark-cli client is nil")
	}
	for _, arg := range args {
		if arg == "--format" || strings.HasPrefix(arg, "--format=") || arg == "--json" {
			return nil, fmt.Errorf("lark-cli output format is owned by the client")
		}
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("wait for lark-cli rate limit: %w", err)
	}
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for lark-cli process slot: %w", ctx.Err())
	}

	commandCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	commandArgs := append([]string(nil), args...)
	commandArgs = append(commandArgs, formatArgs...)
	cmd := exec.CommandContext(commandCtx, c.bin, commandArgs...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	cmd.Env = environmentWithTimezone(c.timezone)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		cause := err
		if commandCtx.Err() != nil {
			cause = commandCtx.Err()
		}
		return stdout.Bytes(), &CommandError{
			Args:     commandArgs,
			ExitCode: exitCode,
			Stderr:   strings.TrimSpace(stderr.String()),
			Cause:    cause,
		}
	}
	return stdout.Bytes(), nil
}

// UserIdentity is the user half of `auth status --verify`: who lark-cli is
// logged in as and whether that login still works against Feishu.
type UserIdentity struct {
	Status      string `json:"status"`
	Available   bool   `json:"available"`
	Verified    bool   `json:"verified"`
	TokenStatus string `json:"tokenStatus"`
	UserName    string `json:"userName"`
	OpenID      string `json:"openId"`
	Message     string `json:"message"`
}

type authStatusResponse struct {
	Identities struct {
		User UserIdentity `json:"user"`
	} `json:"identities"`
}

// VerifyUserIdentity reports the identity behind every `--as user` call. The
// Feishu refresh token expires on its own and lark-cli then drops the stored
// token, so a caller that only checks that the binary exists keeps reporting a
// green state while chat capture, document creation and people search are all
// failing. fail-fast: a CLI failure or an unusable identity is an error.
//
// `auth status` reports local CLI state, not an OpenAPI call: it has no
// {ok:...} envelope and prints JSON without (in fact, rejecting) --format. So
// it reads stdout directly instead of going through Run.
func (c *Client) VerifyUserIdentity(ctx context.Context) (*UserIdentity, error) {
	raw, err := c.runRaw(ctx, "", nil, "auth", "status", "--verify")
	if err != nil {
		return nil, fmt.Errorf("lark-cli auth status: %w", err)
	}
	var response authStatusResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode lark-cli auth status: %w", err)
	}
	user := response.Identities.User
	if user.Status != "ready" || !user.Available || !user.Verified {
		return nil, fmt.Errorf(
			"lark-cli user identity is unusable: status=%q token=%q verified=%t: %s; run `lark-cli auth login --domain all` to re-authorize",
			user.Status, user.TokenStatus, user.Verified, user.Message,
		)
	}
	return &user, nil
}

// MarkdownDocument is the stable subset returned by `docs +create` that the
// product UI needs after a user explicitly clicks export.
type MarkdownDocument struct {
	DocumentID      string
	URL             string
	Warnings        []string
	LinkShareEntity string
}

const tenantEditableLinkShareEntity = "tenant_editable"

type documentPermissionParams struct {
	Token string `json:"token"`
	Type  string `json:"type"`
}

type documentPermissionAuthParams struct {
	Token  string `json:"token"`
	Type   string `json:"type"`
	Action string `json:"action"`
}

type documentPermissionPatch struct {
	LinkShareEntity string `json:"link_share_entity"`
}

type documentPermissionResponse struct {
	Data struct {
		PermissionPublic struct {
			LinkShareEntity string `json:"link_share_entity"`
		} `json:"permission_public"`
	} `json:"data"`
}

// CreateMarkdownDocument creates one document with the lark-cli application
// identity, makes it editable to organization members who have the link, and
// reads the permission back before reporting success. It is intentionally a
// user-triggered effect; schedulers and OKR projection never call it
// implicitly.
func (c *Client) CreateMarkdownDocument(ctx context.Context, title, content string) (MarkdownDocument, error) {
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if title == "" {
		return MarkdownDocument{}, fmt.Errorf("lark-cli create document title is empty")
	}
	if content == "" {
		return MarkdownDocument{}, fmt.Errorf("lark-cli create document content is empty")
	}
	var response struct {
		Data struct {
			Document struct {
				DocumentID string `json:"document_id"`
				URL        string `json:"url"`
			} `json:"document"`
			Warnings []string `json:"warnings"`
		} `json:"data"`
	}
	if err := c.RunInput(ctx, &response, content, "docs", "+create", "--title", title, "--doc-format", "markdown", "--content", "-", "--as", "bot"); err != nil {
		return MarkdownDocument{}, fmt.Errorf("lark-cli create Markdown document: %w", err)
	}
	if strings.TrimSpace(response.Data.Document.DocumentID) == "" || strings.TrimSpace(response.Data.Document.URL) == "" {
		return MarkdownDocument{}, fmt.Errorf("lark-cli create Markdown document returned no document id or url")
	}
	documentID := strings.TrimSpace(response.Data.Document.DocumentID)
	documentURL := strings.TrimSpace(response.Data.Document.URL)
	if err := c.applyExportSecureLabel(ctx, documentID); err != nil {
		return MarkdownDocument{}, fmt.Errorf("label exported document document_id=%q url=%q: %w", documentID, documentURL, err)
	}
	if err := c.setTenantEditableDocumentPermission(ctx, documentID); err != nil {
		return MarkdownDocument{}, fmt.Errorf("configure exported document permission document_id=%q url=%q: %w", documentID, documentURL, err)
	}
	return MarkdownDocument{
		DocumentID:      documentID,
		URL:             documentURL,
		Warnings:        response.Data.Warnings,
		LinkShareEntity: tenantEditableLinkShareEntity,
	}, nil
}

// applyExportSecureLabel stamps the new document with the tenant secure label
// that allows organization-wide link sharing. A freshly created document
// inherits the tenant default label, and Feishu rejects
// link_share_entity=tenant_editable on it with code 91012.
func (c *Client) applyExportSecureLabel(ctx context.Context, documentID string) error {
	if c.exportSecureLabel == "" {
		return fmt.Errorf("lark_cli.export_secure_label is not configured")
	}
	var listResponse struct {
		Data struct {
			Items []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := c.Run(ctx, &listResponse, "drive", "+secure-label-list", "--as", "user"); err != nil {
		return fmt.Errorf("list secure labels: %w", err)
	}
	labelID := ""
	available := make([]string, 0, len(listResponse.Data.Items))
	for _, item := range listResponse.Data.Items {
		available = append(available, item.Name)
		if item.Name == c.exportSecureLabel {
			labelID = item.ID
		}
	}
	if labelID == "" {
		return fmt.Errorf("secure label %q is not available to the current Feishu user; available labels: %s", c.exportSecureLabel, strings.Join(available, ", "))
	}
	var updateResponse struct{}
	if err := c.Run(ctx, &updateResponse, "drive", "+secure-label-update", "--token", documentID, "--type", "docx", "--label-id", labelID, "--as", "user"); err != nil {
		return fmt.Errorf("set secure label %q: %w", c.exportSecureLabel, err)
	}
	return nil
}

func (c *Client) setTenantEditableDocumentPermission(ctx context.Context, documentID string) error {
	paramsJSON, err := json.Marshal(documentPermissionParams{Token: documentID, Type: "docx"})
	if err != nil {
		return fmt.Errorf("encode document permission params: %w", err)
	}
	authParamsJSON, err := json.Marshal(documentPermissionAuthParams{Token: documentID, Type: "docx", Action: "manage_public"})
	if err != nil {
		return fmt.Errorf("encode document permission authorization params: %w", err)
	}
	patchJSON, err := json.Marshal(documentPermissionPatch{LinkShareEntity: tenantEditableLinkShareEntity})
	if err != nil {
		return fmt.Errorf("encode document permission patch: %w", err)
	}

	var authResponse struct {
		Data struct {
			AuthResult bool `json:"auth_result"`
		} `json:"data"`
	}
	if err := c.Run(ctx, &authResponse, "drive", "permission.members", "auth", "--params", string(authParamsJSON), "--as", "bot"); err != nil {
		return fmt.Errorf("check manage_public authorization: %w", err)
	}
	if !authResponse.Data.AuthResult {
		return fmt.Errorf("current Feishu application is not authorized to manage public permissions")
	}

	// lark-cli classifies public permission changes as high risk. The export
	// button is the human confirmation for this exact newly-created document.
	var patchResponse documentPermissionResponse
	if err := c.Run(ctx, &patchResponse, "drive", "permission.public", "patch", "--params", string(paramsJSON), "--data", string(patchJSON), "--as", "bot", "--yes"); err != nil {
		return fmt.Errorf("set link_share_entity=%s: %w", tenantEditableLinkShareEntity, err)
	}

	var getResponse documentPermissionResponse
	if err := c.Run(ctx, &getResponse, "drive", "permission.public", "get", "--params", string(paramsJSON), "--as", "bot"); err != nil {
		return fmt.Errorf("read back document public permission: %w", err)
	}
	if got := getResponse.Data.PermissionPublic.LinkShareEntity; got != tenantEditableLinkShareEntity {
		return fmt.Errorf("document public permission verification failed: got link_share_entity=%q, want %q", got, tenantEditableLinkShareEntity)
	}
	return nil
}

func environmentWithTimezone(timezone string) []string {
	environment := os.Environ()
	result := make([]string, 0, len(environment)+1)
	for _, variable := range environment {
		if strings.HasPrefix(variable, "TZ=") {
			continue
		}
		result = append(result, variable)
	}
	return append(result, "TZ="+timezone)
}
