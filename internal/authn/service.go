// Package authn authenticates the local Jarvis UI with the current ByteDance
// identity exposed by bytedcli. BytedCLI remains the sole owner of SSO tokens.
package authn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

const (
	StatusAuthenticated   = "authenticated"
	StatusUnauthenticated = "unauthenticated"
	StatusPending         = "pending"

	loginFlowTTL = 15 * time.Minute
)

var (
	ErrPending    = errors.New("authentication is still pending")
	ErrNotAllowed = errors.New("this ByteDance identity is not on the allow list")
	ErrDenied     = errors.New("SSO authorization was declined")
)

type User struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	IsPrincipal bool   `json:"-"`
}

type View struct {
	Enabled         bool    `json:"enabled"`
	Status          string  `json:"status"`
	User            *User   `json:"user,omitempty"`
	VerificationURL *string `json:"verification_url,omitempty"`
	UserCode        *string `json:"user_code,omitempty"`
	FlowID          *string `json:"flow_id,omitempty"`
}

type LoginResult struct {
	View
	SessionToken string `json:"-"`
}

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, bin, args...).CombinedOutput()
}

type flow struct {
	token     string
	url       string
	code      string
	profile   string
	expiresAt time.Time
}

type session struct {
	user      User
	expiresAt time.Time
}

type Service struct {
	enabled    bool
	bin        string
	runner     CommandRunner
	sessionTTL time.Duration
	allowed    map[string]bool
	now        func() time.Time

	mu       sync.Mutex
	flows    map[string]flow
	sessions map[string]session
}

func NewService(bin string, sessionTTL time.Duration, enabled bool, allowed []string) (*Service, error) {
	return NewServiceWithRunner(bin, sessionTTL, enabled, allowed, execRunner{})
}

func NewServiceWithRunner(bin string, sessionTTL time.Duration, enabled bool, allowed []string, runner CommandRunner) (*Service, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, fmt.Errorf("authn bytedcli binary is empty")
	}
	if sessionTTL <= 0 {
		return nil, fmt.Errorf("authn session TTL must be positive")
	}
	if runner == nil {
		return nil, fmt.Errorf("authn command runner is nil")
	}
	allowList := make(map[string]bool, len(allowed))
	for _, entry := range allowed {
		if normalized := strings.ToLower(strings.TrimSpace(entry)); normalized != "" {
			allowList[normalized] = true
		}
	}
	if enabled && len(allowList) == 0 {
		return nil, fmt.Errorf("authn is enabled but nobody is on the allow list")
	}
	return &Service{
		enabled:    enabled,
		bin:        strings.TrimSpace(bin),
		runner:     runner,
		sessionTTL: sessionTTL,
		allowed:    allowList,
		now:        time.Now,
		flows:      make(map[string]flow),
		sessions:   make(map[string]session),
	}, nil
}

// allows reports whether an authenticated ByteDance identity may open this
// instance. Both the username and the enterprise email are accepted so the
// configured list can be written in whichever form the operator knows.
func (s *Service) allows(user User) bool {
	return s.allowed[strings.ToLower(strings.TrimSpace(user.Username))] ||
		s.allowed[strings.ToLower(strings.TrimSpace(user.Email))]
}

func (s *Service) Enabled() bool {
	return s.enabled
}

func (s *Service) Status(token string) View {
	if !s.enabled {
		return View{Enabled: false, Status: StatusUnauthenticated}
	}
	user, ok := s.Authenticate(token)
	if !ok {
		return View{Enabled: true, Status: StatusUnauthenticated}
	}
	return View{Enabled: true, Status: StatusAuthenticated, User: &user}
}

func (s *Service) SessionMaxAge() int {
	return int(s.sessionTTL.Seconds())
}

// Login always starts a fresh device flow. It deliberately does not reuse the
// machine's own bytedcli identity: that identity belongs to the host, not to
// whoever opened the page, and handing it out would sign every visitor in as
// the machine owner.
func (s *Service) Login(ctx context.Context) (LoginResult, error) {
	if !s.enabled {
		return LoginResult{View: s.Status("")}, nil
	}
	return s.beginLogin(ctx)
}

func (s *Service) beginLogin(ctx context.Context) (LoginResult, error) {
	flowID, err := randomToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("create SSO flow ID: %w", err)
	}
	// Every browser logs in under its own throwaway bytedcli profile, which
	// isolates credentials and sessions under profiles/<name>/. Without it a
	// visitor's login would overwrite the host's own ByteDance credentials.
	profile := "jarvis-web-" + flowID
	// Jarvis consumes bytedcli's resumable ByteCloud device-flow contract:
	// --begin returns a completion token and verification URL, then --complete
	// finishes the same flow. --session is a different browser-session flow and
	// may legitimately return success without either value when it reuses an
	// existing session.
	raw, err := s.run(ctx, profile, "auth", "login", "--begin")
	if err != nil {
		return LoginResult{}, commandError("start ByteDance SSO", raw, err)
	}
	values := decodeJSONValues(raw)
	token := findString(values, "complete_token", "completeToken", "resume_token", "resumeToken", "device_code", "deviceCode")
	url := findString(values, "verification_uri_complete", "verification_url", "verification_uri", "verify_url")
	code := findString(values, "user_code", "userCode")
	if token == "" || url == "" {
		return LoginResult{}, fmt.Errorf("start ByteDance SSO: response is missing token or verification URL")
	}
	s.mu.Lock()
	s.pruneExpiredFlowsLocked()
	s.flows[flowID] = flow{
		token: token, url: url, code: code, profile: profile,
		expiresAt: s.now().Add(loginFlowTTL),
	}
	s.mu.Unlock()
	return LoginResult{View: View{
		Enabled: true, Status: StatusPending, VerificationURL: stringPointer(url),
		UserCode: optionalString(code), FlowID: stringPointer(flowID),
	}}, nil
}

func (s *Service) Complete(ctx context.Context, flowID string) (LoginResult, error) {
	if !s.enabled {
		return LoginResult{View: s.Status("")}, nil
	}
	flowID = strings.TrimSpace(flowID)
	if flowID == "" {
		return LoginResult{}, fmt.Errorf("SSO flow ID is required")
	}
	pendingFlow, ok := s.pendingFlow(flowID)
	if !ok {
		return s.beginLogin(ctx)
	}

	raw, err := s.run(ctx, pendingFlow.profile, "auth", "login", "--complete", pendingFlow.token)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || hasErrorCode(raw, "AUTHORIZATION_PENDING", "SLOW_DOWN", "BYTECLOUD_AUTH_LOGIN_PENDING", "BYTECLOUD_AUTH_LOGIN_TIMEOUT") {
			return LoginResult{}, ErrPending
		}
		if hasErrorCode(raw, "BYTECLOUD_AUTH_LOGIN_EXPIRED", "BYTECLOUD_AUTH_LOGIN_INVALID_TICKET") {
			s.discardFlow(ctx, flowID, pendingFlow.profile)
			return s.beginLogin(ctx)
		}
		if hasErrorCode(raw, "BYTECLOUD_AUTH_LOGIN_DENIED") {
			s.discardFlow(ctx, flowID, pendingFlow.profile)
			return LoginResult{}, ErrDenied
		}
		// A transport failure must not invalidate an authorization in progress.
		return LoginResult{}, commandError("complete ByteDance SSO", raw, err)
	}
	// A successful CLI invocation can still carry data.status=pending. Only
	// the completion result owns this flow's verified identity; auth status
	// probes unrelated credential systems and can block while awaiting approval.
	var result struct {
		Data struct {
			Status     string `json:"status"`
			AuthStatus struct {
				Authenticated bool `json:"authenticated"`
				Identity      User `json:"identity"`
			} `json:"authStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return LoginResult{}, fmt.Errorf("complete ByteDance SSO: decode response: %w", err)
	}
	switch result.Data.Status {
	case "pending":
		return LoginResult{}, ErrPending
	case "expired", "invalid_ticket":
		s.discardFlow(ctx, flowID, pendingFlow.profile)
		return s.beginLogin(ctx)
	case "denied":
		s.discardFlow(ctx, flowID, pendingFlow.profile)
		return LoginResult{}, ErrDenied
	case "success", "already_authenticated", "not_required":
		// Identity is still required below, even if this profile is already authenticated.
	default:
		return LoginResult{}, fmt.Errorf("complete ByteDance SSO: login status %q", result.Data.Status)
	}
	user := result.Data.AuthStatus.Identity
	user.Username = strings.TrimSpace(user.Username)
	user.Email = strings.TrimSpace(user.Email)
	if !result.Data.AuthStatus.Authenticated || user.Username == "" || user.Email == "" {
		return LoginResult{}, fmt.Errorf("complete ByteDance SSO: verified identity is incomplete")
	}
	// Jarvis only needs to learn who this is. Dropping the throwaway profile
	// keeps the visitor's corporate credentials off this host.
	s.discardFlow(ctx, flowID, pendingFlow.profile)
	if !s.allows(user) {
		return LoginResult{}, fmt.Errorf("%w: %s", ErrNotAllowed, user.Username)
	}
	user.IsPrincipal = true
	return s.startSession(user)
}

// discardFlow forgets a login attempt and clears the bytedcli profile it used.
// Clearing is best effort: a stale profile directory must never block a login
// that already produced a verified identity.
func (s *Service) discardFlow(ctx context.Context, flowID, profile string) {
	s.deleteFlow(flowID)
	if strings.TrimSpace(profile) == "" {
		return
	}
	if raw, err := s.run(ctx, profile, "auth", "clear", "--yes"); err != nil {
		hlog.CtxWarnf(ctx, "clear bytedcli login profile %s failed: %v: %s", profile, err, strings.TrimSpace(string(raw)))
	}
}

func (s *Service) Authenticate(token string) (User, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return User{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[token]
	if !ok {
		return User{}, false
	}
	if !current.expiresAt.After(s.now()) {
		delete(s.sessions, token)
		return User{}, false
	}
	return current.user, true
}

func (s *Service) Logout(token string) {
	s.mu.Lock()
	delete(s.sessions, strings.TrimSpace(token))
	s.mu.Unlock()
}

func (s *Service) deleteFlow(flowID string) {
	s.mu.Lock()
	delete(s.flows, flowID)
	s.mu.Unlock()
}

func (s *Service) pendingFlow(flowID string) (flow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredFlowsLocked()
	pendingFlow, ok := s.flows[flowID]
	return pendingFlow, ok
}

func (s *Service) pruneExpiredFlowsLocked() {
	now := s.now()
	for flowID, pendingFlow := range s.flows {
		if !pendingFlow.expiresAt.After(now) {
			delete(s.flows, flowID)
		}
	}
}

func (s *Service) startSession(user User) (LoginResult, error) {
	token, err := randomToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("create login session: %w", err)
	}
	s.mu.Lock()
	s.sessions[token] = session{user: user, expiresAt: s.now().Add(s.sessionTTL)}
	s.mu.Unlock()
	return LoginResult{
		View:         View{Enabled: true, Status: StatusAuthenticated, User: &user},
		SessionToken: token,
	}, nil
}

func (s *Service) run(ctx context.Context, profile string, args ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := s.runner.Run(runCtx, s.bin, append([]string{"--json", "--profile", profile}, args...)...)
	if err != nil && runCtx.Err() != nil {
		return raw, runCtx.Err()
	}
	return raw, err
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeJSONValues(raw []byte) []any {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	var values []any
	for decoder.More() {
		var value any
		if err := decoder.Decode(&value); err != nil {
			break
		}
		values = append(values, value)
	}
	return values
}

func findValue(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		if found, ok := typed[key]; ok {
			return found
		}
		for _, nested := range typed {
			if found := findValue(nested, key); found != nil {
				return found
			}
		}
	case []any:
		for _, nested := range typed {
			if found := findValue(nested, key); found != nil {
				return found
			}
		}
	}
	return nil
}

func findString(values []any, keys ...string) string {
	for _, key := range keys {
		if value := findValue(values, key); value != nil {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func hasErrorCode(raw []byte, codes ...string) bool {
	code := strings.ToUpper(findString(decodeJSONValues(raw), "code", "error_code", "errorCode"))
	for _, candidate := range codes {
		if code == candidate {
			return true
		}
	}
	return false
}

func commandError(action string, raw []byte, err error) error {
	values := decodeJSONValues(raw)
	// CLI progress events can precede the result. Their generic "retry with
	// --debug" hint must not hide the actual failure in the result envelope.
	for _, value := range values {
		if envelope, ok := value.(map[string]any); ok {
			if failure, ok := envelope["error"].(map[string]any); ok {
				if message, ok := failure["message"].(string); ok && strings.TrimSpace(message) != "" {
					return fmt.Errorf("%s: %s", action, message)
				}
			}
		}
	}
	if message := findString(values, "message", "detail", "hint"); message != "" {
		return fmt.Errorf("%s: %s", action, message)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func stringPointer(value string) *string {
	return &value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
