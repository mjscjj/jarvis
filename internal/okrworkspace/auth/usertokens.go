package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// refreshLeeway refreshes slightly early so a token handed to an agent does not
// expire in the middle of the work it was handed out for.
const refreshLeeway = 5 * time.Minute

var (
	// ErrNoUserToken means this person has no stored grant at all: nobody has
	// signed in as them, or their file was removed.
	ErrNoUserToken = errors.New("this person has no stored Feishu token; sign in to Biz OKR again")
	// ErrUserTokenUnusable means a grant exists but can no longer produce a
	// working access token, so only a fresh login can fix it.
	ErrUserTokenUnusable = errors.New("the stored Feishu token can no longer be refreshed; sign in to Biz OKR again")
)

// UserTokens hands out a currently valid Feishu access token for one person,
// refreshing it from the stored refresh token when it is close to expiry. It is
// the read side of what the device login writes.
type UserTokens struct {
	store    *TokenStore
	provider Provider
	now      func() time.Time

	// mu keeps two concurrent turns from refreshing the same person twice.
	mu sync.Mutex
}

func NewUserTokens(store *TokenStore, provider Provider) (*UserTokens, error) {
	if store == nil {
		return nil, fmt.Errorf("create user tokens: token store is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("create user tokens: provider is required")
	}
	return &UserTokens{store: store, provider: provider, now: time.Now}, nil
}

// Ensure returns a usable token for openID, refreshing and rewriting the stored
// file when needed. fail-fast: it never returns a token it believes is expired.
func (u *UserTokens) Ensure(ctx context.Context, openID string) (StoredToken, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return StoredToken{}, fmt.Errorf("ensure Feishu token: open_id is required")
	}
	u.mu.Lock()
	defer u.mu.Unlock()

	stored, found, err := u.store.Load(openID)
	if err != nil {
		return StoredToken{}, err
	}
	if !found {
		return StoredToken{}, fmt.Errorf("%w: %s", ErrNoUserToken, openID)
	}
	now := u.now().UTC()
	if stored.ExpiresAt == nil || stored.ExpiresAt.After(now.Add(refreshLeeway)) {
		return stored, nil
	}
	if strings.TrimSpace(stored.RefreshToken) == "" {
		return StoredToken{}, fmt.Errorf("%w: %s has an expired access token and no refresh token", ErrUserTokenUnusable, openID)
	}
	if stored.RefreshExpiresAt != nil && !stored.RefreshExpiresAt.After(now) {
		return StoredToken{}, fmt.Errorf("%w: %s refresh token expired at %s", ErrUserTokenUnusable, openID, stored.RefreshExpiresAt.Format(time.RFC3339))
	}
	grant, err := u.provider.RefreshGrant(ctx, stored.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrDeviceAuthorizationExpired) {
			return StoredToken{}, fmt.Errorf("%w: %s: %v", ErrUserTokenUnusable, openID, err)
		}
		return StoredToken{}, fmt.Errorf("refresh Feishu token of %s: %w", openID, err)
	}
	if grant.User.OpenID != openID {
		return StoredToken{}, fmt.Errorf("refresh Feishu token of %s: Feishu returned identity %q", openID, grant.User.OpenID)
	}
	// Feishu may return a rotated refresh token or none at all; carry the
	// previous refresh token and its deadline forward when they are not
	// replaced, otherwise the next refresh has nothing to present.
	if strings.TrimSpace(grant.RefreshToken) == "" {
		grant.RefreshToken = stored.RefreshToken
		if grant.RefreshExpiresIn <= 0 && stored.RefreshExpiresAt != nil {
			grant.RefreshExpiresIn = stored.RefreshExpiresAt.Sub(now)
		}
	}
	return u.store.Save(grant, now)
}
