package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TokenStore keeps the Feishu tokens of everyone who signed in, one JSON file
// per open_id. The browser session itself only needs the open_id, so this file
// is the place where later features (calling Feishu as the signed-in user)
// pick up their access and refresh tokens.
type TokenStore struct {
	dir string
}

// StoredToken is the full on-disk record. It stays a loose JSON document: new
// fields from Feishu can be added without a migration.
type StoredToken struct {
	OpenID           string     `json:"open_id"`
	Name             string     `json:"name"`
	Email            string     `json:"email,omitempty"`
	AvatarURL        string     `json:"avatar_url,omitempty"`
	AccessToken      string     `json:"access_token"`
	RefreshToken     string     `json:"refresh_token,omitempty"`
	TokenType        string     `json:"token_type,omitempty"`
	Scope            string     `json:"scope,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	RefreshExpiresAt *time.Time `json:"refresh_expires_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func NewTokenStore(dir string) (*TokenStore, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("create OKR token store: directory is required")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve OKR token directory %q: %w", dir, err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create OKR token directory %q: %w", absolute, err)
	}
	return &TokenStore{dir: absolute}, nil
}

func (s *TokenStore) Dir() string { return s.dir }

// Save writes one grant, replacing whatever that person had before. A second
// login by the same person simply overwrites their file with fresher tokens.
func (s *TokenStore) Save(grant Grant, now time.Time) (StoredToken, error) {
	path, err := s.path(grant.User.OpenID)
	if err != nil {
		return StoredToken{}, err
	}
	now = now.UTC()
	record := StoredToken{
		OpenID:       grant.User.OpenID,
		Name:         grant.User.Name,
		Email:        grant.User.Email,
		AvatarURL:    grant.User.AvatarURL,
		AccessToken:  grant.AccessToken,
		RefreshToken: grant.RefreshToken,
		TokenType:    grant.TokenType,
		Scope:        grant.Scope,
		UpdatedAt:    now,
	}
	if grant.ExpiresIn > 0 {
		expiresAt := now.Add(grant.ExpiresIn)
		record.ExpiresAt = &expiresAt
	}
	if grant.RefreshExpiresIn > 0 {
		refreshExpiresAt := now.Add(grant.RefreshExpiresIn)
		record.RefreshExpiresAt = &refreshExpiresAt
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return StoredToken{}, fmt.Errorf("encode Feishu token of %s: %w", record.OpenID, err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		return StoredToken{}, fmt.Errorf("write Feishu token file %q: %w", path, err)
	}
	return record, nil
}

// Load reads back one person's tokens. It reports whether the file exists so a
// caller can tell "never signed in" apart from a real read failure.
func (s *TokenStore) Load(openID string) (StoredToken, bool, error) {
	path, err := s.path(openID)
	if err != nil {
		return StoredToken{}, false, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return StoredToken{}, false, nil
	}
	if err != nil {
		return StoredToken{}, false, fmt.Errorf("read Feishu token file %q: %w", path, err)
	}
	var record StoredToken
	if err := json.Unmarshal(raw, &record); err != nil {
		return StoredToken{}, false, fmt.Errorf("decode Feishu token file %q: %w", path, err)
	}
	return record, true, nil
}

// path maps an open_id to its file. Feishu open_ids are alphanumeric with
// underscores; anything else is rejected rather than sanitized, so a malformed
// id can never resolve to a file outside the directory.
func (s *TokenStore) path(openID string) (string, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return "", fmt.Errorf("Feishu token file name: open_id is required")
	}
	for _, char := range openID {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9', char == '_', char == '-':
		default:
			return "", fmt.Errorf("Feishu token file name: open_id %q has unexpected character %q", openID, char)
		}
	}
	return filepath.Join(s.dir, openID+".json"), nil
}
