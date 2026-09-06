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
)

const (
	StatusAuthenticated   = "authenticated"
	StatusUnauthenticated = "unauthenticated"
	StatusPending         = "pending"
)

var ErrPending = errors.New("authentication is still pending")

type User struct {
	Username string `json:"username"`
	Email    string `json:"email"`
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
	token string
	url   string
	code  string
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
	now        func() time.Time

	mu       sync.Mutex
	flows    map[string]flow
	sessions map[string]session
}

func NewService(bin string, sessionTTL time.Duration, enabled bool) (*Service, error) {
	return NewServiceWithRunner(bin, sessionTTL, enabled, execRunner{})
}

func NewServiceWithRunner(bin string, sessionTTL time.Duration, enabled bool, runner CommandRunner) (*Service, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, fmt.Errorf("authn bytedcli binary is empty")
	}
	if sessionTTL <= 0 {
		return nil, fmt.Errorf("authn session TTL must be positive")
	}
	if runner == nil {
		return nil, fmt.Errorf("authn command runner is nil")
	}
	return &Service{
		enabled:    enabled,
		bin:        strings.TrimSpace(bin),
		runner:     runner,
		sessionTTL: sessionTTL,
		now:        time.Now,
		flows:      make(map[string]flow),
		sessions:   make(map[string]session),
	}, nil
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

func (s *Service) Login(ctx context.Context) (LoginResult, error) {
	if !s.enabled {
		return LoginResult{View: s.Status("")}, nil
	}
	user, authenticated, err := s.probe(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	if authenticated {
		return s.startSession(user)
	}

	// Jarvis consumes bytedcli's resumable ByteCloud device-flow contract:
	// --begin returns a completion token and verification URL, then --complete
	// finishes the same flow. --session is a different browser-session flow and
	// may legitimately return success without either value when it reuses an
	// existing session.
	raw, err := s.run(ctx, "--json", "auth", "login", "--begin")
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
	flowID, err := randomToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("create SSO flow ID: %w", err)
	}
	s.mu.Lock()
	s.flows[flowID] = flow{token: token, url: url, code: code}
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
	s.mu.Lock()
	pendingFlow, ok := s.flows[strings.TrimSpace(flowID)]
	s.mu.Unlock()
	if !ok {
		return LoginResult{}, fmt.Errorf("SSO flow not found or expired")
	}

	raw, err := s.run(ctx, "--json", "auth", "login", "--complete", pendingFlow.token)
	if err != nil {
		if hasErrorCode(raw, "AUTHORIZATION_PENDING", "SLOW_DOWN") {
			return LoginResult{}, ErrPending
		}
		return LoginResult{}, commandError("complete ByteDance SSO", raw, err)
	}
	user, authenticated, err := s.probe(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	if !authenticated {
		return LoginResult{}, ErrPending
	}
	s.mu.Lock()
	delete(s.flows, strings.TrimSpace(flowID))
	s.mu.Unlock()
	return s.startSession(user)
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

func (s *Service) probe(ctx context.Context) (User, bool, error) {
	raw, err := s.run(ctx, "--json", "auth", "status")
	if err != nil {
		return User{}, false, commandError("read ByteDance SSO status", raw, err)
	}
	var status struct {
		Data struct {
			Authenticated bool `json:"authenticated"`
			ByteCloudAuth struct {
				Identity User `json:"identity"`
			} `json:"bytecloud_auth"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return User{}, false, fmt.Errorf("read ByteDance SSO status: decode response: %w", err)
	}
	if !status.Data.Authenticated {
		return User{}, false, nil
	}
	user := status.Data.ByteCloudAuth.Identity
	user.Username = strings.TrimSpace(user.Username)
	user.Email = strings.TrimSpace(user.Email)
	if user.Username == "" || user.Email == "" {
		return User{}, false, fmt.Errorf("read ByteDance SSO status: authenticated identity is incomplete")
	}
	return user, true, nil
}

func (s *Service) run(ctx context.Context, args ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return s.runner.Run(runCtx, s.bin, args...)
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
	if message := findString(decodeJSONValues(raw), "message", "detail", "hint"); message != "" {
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
