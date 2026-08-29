package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"gorm.io/gorm"
)

const (
	CookieName      = "jarvis_okr_session"
	oauthStateTTL   = 10 * time.Minute
	defaultReturnTo = "/#/okr"
)

var ErrUnauthenticated = errors.New("not signed in")

type Session struct {
	User      User      `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Service struct {
	db       *gorm.DB
	cfg      moduleconfig.IdentityConfig
	provider Provider
	now      func() time.Time
}

func NewService(db *gorm.DB, cfg moduleconfig.IdentityConfig, provider Provider) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("create OKR auth service: database is required")
	}
	if cfg.Enabled && provider == nil {
		return nil, fmt.Errorf("create OKR auth service: provider is required when enabled")
	}
	return &Service{db: db, cfg: cfg, provider: provider, now: time.Now}, nil
}

func (s *Service) Enabled() bool      { return s.cfg.Enabled }
func (s *Service) CookieSecure() bool { return s.cfg.CookieSecure }
func (s *Service) SessionMaxAge() int {
	if s.cfg.SessionTTLHours <= 0 {
		return 0
	}
	return int((time.Duration(s.cfg.SessionTTLHours) * time.Hour) / time.Second)
}

func (s *Service) BeginLogin(ctx context.Context, returnTo string) (string, error) {
	if !s.Enabled() {
		return "", fmt.Errorf("Feishu login is not configured")
	}
	state, err := randomToken()
	if err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	now := s.now().UTC()
	row := domain.OAuthState{StateHash: digest(state), ReturnTo: safeReturnTo(returnTo), ExpiresAt: now.Add(oauthStateTTL), CreatedAt: now}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at < ?", now).Delete(&domain.OAuthState{}).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	}); err != nil {
		return "", fmt.Errorf("store OAuth state: %w", err)
	}
	return s.provider.AuthorizationURL(state), nil
}

func (s *Service) CompleteLogin(ctx context.Context, code, state string) (Session, string, string, error) {
	if !s.Enabled() {
		return Session{}, "", "", fmt.Errorf("Feishu login is not configured")
	}
	code, state = strings.TrimSpace(code), strings.TrimSpace(state)
	if code == "" || state == "" {
		return Session{}, "", "", fmt.Errorf("OAuth code and state are required")
	}
	now := s.now().UTC()
	var stateRow domain.OAuthState
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&stateRow, "state_hash = ?", digest(state)).Error; err != nil {
			return err
		}
		if err := tx.Delete(&domain.OAuthState{}, "state_hash = ?", stateRow.StateHash).Error; err != nil {
			return err
		}
		if !stateRow.ExpiresAt.After(now) {
			return fmt.Errorf("OAuth state expired")
		}
		return nil
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Session{}, "", "", fmt.Errorf("OAuth state is invalid or already used")
		}
		return Session{}, "", "", err
	}

	user, err := s.provider.ExchangeCode(ctx, code)
	if err != nil {
		return Session{}, "", "", err
	}
	token, err := randomToken()
	if err != nil {
		return Session{}, "", "", fmt.Errorf("generate session: %w", err)
	}
	expiresAt := now.Add(time.Duration(s.cfg.SessionTTLHours) * time.Hour)
	row := domain.AuthSession{TokenHash: digest(token), OpenID: user.OpenID, Name: user.Name, AvatarURL: user.AvatarURL, Email: user.Email, ExpiresAt: expiresAt, CreatedAt: now, LastSeenAt: now}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at < ?", now).Delete(&domain.AuthSession{}).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	}); err != nil {
		return Session{}, "", "", fmt.Errorf("create session: %w", err)
	}
	return Session{User: user, ExpiresAt: expiresAt}, token, safeReturnTo(stateRow.ReturnTo), nil
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
	return Session{User: User{OpenID: row.OpenID, Name: row.Name, AvatarURL: row.AvatarURL, Email: row.Email}, ExpiresAt: row.ExpiresAt}, nil
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

func safeReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\r\n") {
		return defaultReturnTo
	}
	return value
}
