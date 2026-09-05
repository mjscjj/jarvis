package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	AuthAuthorized  = "authorized"
	AuthRequired    = "required"
	AuthUnavailable = "unavailable"
	AuthPending     = "pending"
	AuthFailed      = "failed"
)

type AuthStatus struct {
	Status          string  `json:"status"`
	VerificationURL *string `json:"verification_url"`
	UserCode        *string `json:"user_code"`
	FlowID          *string `json:"flow_id"`
	Error           *string `json:"error"`
}

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, bin, args...).CombinedOutput()
}

type authFlow struct {
	ID       string
	Provider string
	Token    string
	URL      string
	UserCode string
}

type Authorizer struct {
	runner commandRunner
	mu     sync.Mutex
	flows  map[string]authFlow
	now    func() time.Time
}

func NewAuthorizer() *Authorizer {
	return &Authorizer{runner: execRunner{}, flows: make(map[string]authFlow), now: time.Now}
}

func newAuthorizer(runner commandRunner) *Authorizer {
	return &Authorizer{runner: runner, flows: make(map[string]authFlow), now: time.Now}
}

func (a *Authorizer) Probe(ctx context.Context, provider string) AuthStatus {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var bin string
	var args []string
	switch provider {
	case "bytedcli-session":
		bin = "bytedcli"
		args = []string{"--json", "auth", "status"}
	case "meego":
		bin = "bytedcli"
		args = []string{"--json", "meego", "status"}
	case "lark-cli-im":
		bin = "lark-cli"
		args = []string{"im", "+chat-search", "--as", "user", "--query", "oncall", "--disable-search-by-user", "--chat-modes", "group,topic", "--search-types", "private,public_joined", "--page-size", "1", "--page-limit", "1", "--format", "json"}
	default:
		return authError(AuthUnavailable, fmt.Sprintf("unknown authorization provider %q", provider))
	}
	raw, err := a.runner.Run(ctx, bin, args...)
	if err == nil && probeSucceeded(provider, raw) {
		return AuthStatus{Status: AuthAuthorized}
	}
	if err == nil || hasErrorCode(raw, "AUTH_REQUIRED", "MEEGLE_AUTH_REQUIRED", "MISSING_SCOPE") {
		return AuthStatus{Status: AuthRequired}
	}
	return authError(AuthUnavailable, commandError(raw, err))
}

func (a *Authorizer) Begin(ctx context.Context, provider string) AuthStatus {
	if status := a.Probe(ctx, provider); status.Status == AuthAuthorized {
		return status
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	bin := "bytedcli"
	args := []string{"--json"}
	switch provider {
	case "bytedcli-session":
		args = append(args, "auth", "login", "--begin", "--session")
	case "lark-cli-im":
		bin = "lark-cli"
		args = []string{"auth", "login", "--domain", "im", "--no-wait", "--json"}
	case "meego":
		args = append(args, "meego", "login", "--begin")
	default:
		return authError(AuthUnavailable, fmt.Sprintf("unknown authorization provider %q", provider))
	}
	raw, err := a.runner.Run(ctx, bin, args...)
	if err != nil {
		return authError(AuthFailed, commandError(raw, err))
	}
	payloads := decodeJSONValues(raw)
	if len(payloads) == 0 {
		return authError(AuthFailed, "authorization command returned invalid data")
	}
	token := findString(payloads, "complete_token", "completeToken", "resume_token", "resumeToken", "device_code", "deviceCode")
	url := findString(payloads, "verification_uri_complete", "verification_url", "verification_uri", "verify_url")
	userCode := findString(payloads, "user_code", "userCode")
	if token == "" || url == "" {
		return authError(AuthFailed, "authorization response is missing token or verification URL")
	}
	flowID := fmt.Sprintf("%d", a.now().UnixNano())
	a.mu.Lock()
	a.flows[flowID] = authFlow{ID: flowID, Provider: provider, Token: token, URL: url, UserCode: userCode}
	a.mu.Unlock()
	return AuthStatus{
		Status: AuthPending, VerificationURL: stringPointer(url),
		UserCode: optionalString(userCode), FlowID: stringPointer(flowID),
	}
}

func (a *Authorizer) Complete(ctx context.Context, provider, flowID string) AuthStatus {
	a.mu.Lock()
	flow, ok := a.flows[strings.TrimSpace(flowID)]
	a.mu.Unlock()
	if !ok {
		return authError(AuthFailed, "authorization flow not found or expired")
	}
	if flow.Provider != provider {
		return authError(AuthFailed, "authorization flow does not belong to this plugin")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	bin := "bytedcli"
	args := []string{"--json"}
	switch flow.Provider {
	case "bytedcli-session":
		args = append(args, "auth", "login", "--complete", flow.Token)
	case "meego":
		args = append(args, "meego", "login", "--complete", flow.Token)
	case "lark-cli-im":
		bin = "lark-cli"
		args = []string{"auth", "login", "--domain", "im", "--device-code", flow.Token, "--json"}
	default:
		return authError(AuthFailed, "authorization provider cannot be completed")
	}
	raw, err := a.runner.Run(ctx, bin, args...)
	if err == nil {
		a.mu.Lock()
		delete(a.flows, flow.ID)
		a.mu.Unlock()
		return AuthStatus{Status: AuthAuthorized}
	}
	if hasErrorCode(raw, "AUTHORIZATION_PENDING", "SLOW_DOWN") {
		return AuthStatus{
			Status: AuthPending, VerificationURL: stringPointer(flow.URL),
			UserCode: optionalString(flow.UserCode), FlowID: stringPointer(flow.ID),
		}
	}
	return authError(AuthFailed, commandError(raw, err))
}

func probeSucceeded(provider string, raw []byte) bool {
	payloads := decodeJSONValues(raw)
	if len(payloads) == 0 {
		return false
	}
	switch provider {
	case "bytedcli-session", "meego":
		value := findValue(payloads, "authenticated")
		authenticated, _ := value.(bool)
		return authenticated
	case "lark-cli-im":
		return findValue(payloads, "chats") != nil
	default:
		for _, payload := range payloads {
			if envelopeSucceeded(payload) {
				return true
			}
		}
		return false
	}
}

func envelopeSucceeded(payload any) bool {
	object, ok := payload.(map[string]any)
	if !ok {
		return false
	}
	status, _ := object["status"].(string)
	if strings.EqualFold(status, "success") {
		return true
	}
	okValue, _ := object["ok"].(bool)
	return okValue
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

func commandError(raw []byte, err error) string {
	payloads := decodeJSONValues(raw)
	if message := findString(payloads, "message", "detail", "hint"); message != "" {
		return message
	}
	if err != nil {
		return err.Error()
	}
	return "authorization command failed"
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

func findString(value any, keys ...string) string {
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	var visit func(any) string
	visit = func(current any) string {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				if _, ok := wanted[key]; ok {
					if text, ok := nested.(string); ok && strings.TrimSpace(text) != "" {
						return strings.TrimSpace(text)
					}
				}
			}
			for _, nested := range typed {
				if found := visit(nested); found != "" {
					return found
				}
			}
		case []any:
			for _, nested := range typed {
				if found := visit(nested); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return visit(value)
}

func authError(status, message string) AuthStatus {
	return AuthStatus{Status: status, Error: stringPointer(strings.TrimSpace(message))}
}

func stringPointer(value string) *string { return &value }

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return stringPointer(value)
}
