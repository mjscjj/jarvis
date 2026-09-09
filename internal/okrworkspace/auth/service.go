package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"gorm.io/gorm"
)

const CookieName = "jarvis_okr_session"

const (
	DeviceLoginPending   = "pending"
	DeviceLoginCompleted = "completed"
	DeviceLoginDenied    = "denied"
	DeviceLoginExpired   = "expired"
)

var (
	ErrUnauthenticated     = errors.New("not signed in")
	ErrDeviceLoginNotFound = errors.New("device login is invalid or already completed")
)

type Session struct {
	User      User      `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
}

type DeviceLogin struct {
	ID                  string    `json:"login_id"`
	VerificationURL     string    `json:"verification_url"`
	UserCode            string    `json:"user_code,omitempty"`
	ExpiresAt           time.Time `json:"expires_at"`
	PollIntervalSeconds int       `json:"poll_interval_seconds"`
}

type DeviceLoginPoll struct {
	Status            string `json:"status"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
	User              *User  `json:"user,omitempty"`
}

type pendingDeviceLogin struct {
	deviceCode   string
	expiresAt    time.Time
	nextPollAt   time.Time
	pollInterval time.Duration
}

type Service struct {
	db       *gorm.DB
	cfg      moduleconfig.IdentityConfig
	provider Provider
	tokens   *TokenStore
	now      func() time.Time

	deviceMu     sync.Mutex
	deviceLogins map[string]pendingDeviceLogin
}

func NewService(db *gorm.DB, cfg moduleconfig.IdentityConfig, provider Provider, tokens *TokenStore) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("create OKR auth service: database is required")
	}
	if cfg.Enabled && provider == nil {
		return nil, fmt.Errorf("create OKR auth service: provider is required when enabled")
	}
	if cfg.Enabled && tokens == nil {
		return nil, fmt.Errorf("create OKR auth service: token store is required when enabled")
	}
	return &Service{db: db, cfg: cfg, provider: provider, tokens: tokens, now: time.Now, deviceLogins: make(map[string]pendingDeviceLogin)}, nil
}

func (s *Service) Enabled() bool      { return s.cfg.Enabled }
func (s *Service) CookieSecure() bool { return s.cfg.CookieSecure }
func (s *Service) SessionMaxAge() int {
	if s.cfg.SessionTTLHours <= 0 {
		return 0
	}
	return int((time.Duration(s.cfg.SessionTTLHours) * time.Hour) / time.Second)
}

func (s *Service) BeginDeviceLogin(ctx context.Context) (DeviceLogin, error) {
	if !s.Enabled() {
		return DeviceLogin{}, fmt.Errorf("Feishu login is not configured")
	}
	authorization, err := s.provider.RequestDeviceAuthorization(ctx)
	if err != nil {
		return DeviceLogin{}, err
	}
	loginID, err := randomToken()
	if err != nil {
		return DeviceLogin{}, fmt.Errorf("generate device login id: %w", err)
	}
	now := s.now().UTC()
	expiresAt := now.Add(authorization.ExpiresIn)
	pollInterval := authorization.PollInterval
	if pollInterval < time.Second {
		return DeviceLogin{}, fmt.Errorf("Feishu device authorization returned invalid poll interval %s", pollInterval)
	}

	s.deviceMu.Lock()
	for id, pending := range s.deviceLogins {
		if !pending.expiresAt.After(now) {
			delete(s.deviceLogins, id)
		}
	}
	s.deviceLogins[loginID] = pendingDeviceLogin{
		deviceCode:   authorization.DeviceCode,
		expiresAt:    expiresAt,
		nextPollAt:   now.Add(pollInterval),
		pollInterval: pollInterval,
	}
	s.deviceMu.Unlock()

	return DeviceLogin{
		ID:                  loginID,
		VerificationURL:     authorization.VerificationURL,
		UserCode:            authorization.UserCode,
		ExpiresAt:           expiresAt,
		PollIntervalSeconds: durationSeconds(pollInterval),
	}, nil
}

func (s *Service) PollDeviceLogin(ctx context.Context, loginID string) (DeviceLoginPoll, string, error) {
	if !s.Enabled() {
		return DeviceLoginPoll{}, "", fmt.Errorf("Feishu login is not configured")
	}
	loginID = strings.TrimSpace(loginID)
	if loginID == "" {
		return DeviceLoginPoll{}, "", ErrDeviceLoginNotFound
	}
	now := s.now().UTC()

	s.deviceMu.Lock()
	pending, ok := s.deviceLogins[loginID]
	if !ok {
		s.deviceMu.Unlock()
		// Device authorizations are intentionally process-local. A restart while
		// the user is approving in Feishu therefore loses this entry. Report the
		// flow as expired so the browser can offer a fresh login instead of
		// surfacing an internal 404 as a page navigation error.
		return DeviceLoginPoll{Status: DeviceLoginExpired}, "", nil
	}
	if !pending.expiresAt.After(now) {
		delete(s.deviceLogins, loginID)
		s.deviceMu.Unlock()
		return DeviceLoginPoll{Status: DeviceLoginExpired}, "", nil
	}
	if pending.nextPollAt.After(now) {
		retryAfter := durationSeconds(pending.nextPollAt.Sub(now))
		s.deviceMu.Unlock()
		return DeviceLoginPoll{Status: DeviceLoginPending, RetryAfterSeconds: retryAfter}, "", nil
	}
	pending.nextPollAt = now.Add(pending.pollInterval)
	s.deviceLogins[loginID] = pending
	s.deviceMu.Unlock()

	grant, err := s.provider.PollDeviceAuthorization(ctx, pending.deviceCode)
	switch {
	case errors.Is(err, ErrDeviceAuthorizationPending):
		return DeviceLoginPoll{Status: DeviceLoginPending, RetryAfterSeconds: durationSeconds(pending.pollInterval)}, "", nil
	case errors.Is(err, ErrDeviceAuthorizationSlowDown):
		pending.pollInterval += 5 * time.Second
		if pending.pollInterval > time.Minute {
			pending.pollInterval = time.Minute
		}
		pending.nextPollAt = now.Add(pending.pollInterval)
		s.deviceMu.Lock()
		if _, exists := s.deviceLogins[loginID]; exists {
			s.deviceLogins[loginID] = pending
		}
		s.deviceMu.Unlock()
		return DeviceLoginPoll{Status: DeviceLoginPending, RetryAfterSeconds: durationSeconds(pending.pollInterval)}, "", nil
	case errors.Is(err, ErrDeviceAuthorizationDenied):
		s.deleteDeviceLogin(loginID)
		return DeviceLoginPoll{Status: DeviceLoginDenied}, "", nil
	case errors.Is(err, ErrDeviceAuthorizationExpired):
		s.deleteDeviceLogin(loginID)
		return DeviceLoginPoll{Status: DeviceLoginExpired}, "", nil
	case err != nil:
		return DeviceLoginPoll{}, "", err
	}

	s.deleteDeviceLogin(loginID)
	if _, err := s.tokens.Save(grant, now); err != nil {
		return DeviceLoginPoll{}, "", err
	}
	session, token, err := s.createSession(ctx, grant.User, now)
	if err != nil {
		return DeviceLoginPoll{}, "", err
	}
	return DeviceLoginPoll{Status: DeviceLoginCompleted, User: &session.User}, token, nil
}

func (s *Service) createSession(ctx context.Context, user User, now time.Time) (Session, string, error) {
	if strings.TrimSpace(user.OpenID) == "" {
		return Session{}, "", fmt.Errorf("create session: Feishu user open_id is empty")
	}
	if strings.TrimSpace(user.UnionID) == "" {
		return Session{}, "", fmt.Errorf("create session: Feishu user union_id is empty")
	}
	token, err := randomToken()
	if err != nil {
		return Session{}, "", fmt.Errorf("generate session: %w", err)
	}
	expiresAt := now.Add(time.Duration(s.cfg.SessionTTLHours) * time.Hour)
	row := domain.AuthSession{TokenHash: digest(token), OpenID: user.OpenID, UnionID: user.UnionID, Name: user.Name, AvatarURL: user.AvatarURL, Email: user.Email, ExpiresAt: expiresAt, CreatedAt: now, LastSeenAt: now}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at < ?", now).Delete(&domain.AuthSession{}).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	}); err != nil {
		return Session{}, "", fmt.Errorf("create session: %w", err)
	}
	return Session{User: user, ExpiresAt: expiresAt}, token, nil
}

func (s *Service) deleteDeviceLogin(loginID string) {
	s.deviceMu.Lock()
	delete(s.deviceLogins, loginID)
	s.deviceMu.Unlock()
}

func (s *Service) Current(ctx context.Context, token string) (Session, error) {
	if !s.Enabled() {
		return Session{}, ErrUnauthenticated
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Session{}, ErrUnauthenticated
	}
	now := s.now().UTC()
	var row domain.AuthSession
	if err := s.db.WithContext(ctx).First(&row, "token_hash = ?", digest(token)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Session{}, ErrUnauthenticated
		}
		return Session{}, fmt.Errorf("read session: %w", err)
	}
	if !row.ExpiresAt.After(now) {
		_ = s.db.WithContext(ctx).Delete(&domain.AuthSession{}, "token_hash = ?", row.TokenHash).Error
		return Session{}, ErrUnauthenticated
	}
	if now.Sub(row.LastSeenAt) >= 5*time.Minute {
		_ = s.db.WithContext(ctx).Model(&domain.AuthSession{}).Where("token_hash = ?", row.TokenHash).Update("last_seen_at", now).Error
	}
	return Session{User: User{OpenID: row.OpenID, UnionID: row.UnionID, Name: row.Name, AvatarURL: row.AvatarURL, Email: row.Email}, ExpiresAt: row.ExpiresAt}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if err := s.db.WithContext(ctx).Delete(&domain.AuthSession{}, "token_hash = ?", digest(token)).Error; err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func durationSeconds(value time.Duration) int {
	return max(1, int(math.Ceil(value.Seconds())))
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
