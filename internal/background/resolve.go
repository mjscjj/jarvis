package background

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"jarvis/internal/larkcli"
)

// userSearcher is the lark-cli subset this package needs. Declared here (not
// depending on the concrete client) so the resolver stays unit-testable.
type userSearcher interface {
	SearchUser(ctx context.Context, query string) ([]larkcli.UserCandidate, bool, error)
	SearchUserAvatars(ctx context.Context, query string) ([]larkcli.UserAvatar, error)
}

// ResolveCandidate is the API projection of one lark-cli search hit. It carries
// exactly the fields the person form needs to auto-fill on selection.
type ResolveCandidate struct {
	OpenID     string `json:"open_id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Department string `json:"department"`
	P2PChatID  string `json:"p2p_chat_id"`
	IsExternal bool   `json:"is_external"`
	HasChatted bool   `json:"has_chatted"`
}

// ResolveResult wraps the candidate list with lark-cli's has_more flag so the
// UI can tell the user to narrow an ambiguous query instead of guessing.
type ResolveResult struct {
	Candidates []ResolveCandidate `json:"candidates"`
	HasMore    bool               `json:"has_more"`
}

// PersonAvatar is one person's display picture. Callers match it to a stored
// owner by open_id; name is carried so a caller that only knows a name (a
// comment author, say) can still line the two up.
type PersonAvatar struct {
	OpenID    string `json:"open_id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// ResolveService turns a name/email query into feishu open_id candidates so the
// person form never asks the user to type a raw ou_xxx id.
type ResolveService struct {
	search userSearcher

	// avatars memoizes one lark-cli lookup per queried name. An OKR board
	// carries dozens of owners and lark-cli only runs two calls at a time, so
	// without a cache one page load would monopolize the CLI for a minute. It
	// is mirrored to avatarFile because avatars barely ever change and the
	// server restarts often.
	mu         sync.Mutex
	avatars    map[string][]PersonAvatar
	avatarFile string
}

// NewResolveService wires the people resolver. avatarFile is where resolved
// avatars are kept across restarts; fail-fast: it must be set, and an existing
// file that cannot be read or parsed is an error rather than a silent reset.
func NewResolveService(search userSearcher, avatarFile string) (*ResolveService, error) {
	if search == nil {
		return nil, fmt.Errorf("resolve service searcher is nil")
	}
	if strings.TrimSpace(avatarFile) == "" {
		return nil, fmt.Errorf("resolve service avatar cache file is empty")
	}
	service := &ResolveService{search: search, avatars: map[string][]PersonAvatar{}, avatarFile: avatarFile}
	raw, err := os.ReadFile(avatarFile)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return service, nil
	case err != nil:
		return nil, fmt.Errorf("read avatar cache %q: %w", avatarFile, err)
	}
	if err := json.Unmarshal(raw, &service.avatars); err != nil {
		return nil, fmt.Errorf("parse avatar cache %q: %w", avatarFile, err)
	}
	return service, nil
}

// Avatars resolves the avatar of every named person, one lark-cli lookup per
// distinct name, run in parallel under the client's own rate limit. fail-fast:
// a blank list is rejected and the first CLI failure surfaces unchanged, so a
// dead user token shows up as an error instead of silently empty avatars.
func (s *ResolveService) Avatars(ctx context.Context, names []string) ([]PersonAvatar, error) {
	wanted := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		clean := strings.TrimSpace(name)
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		wanted = append(wanted, clean)
	}
	if len(wanted) == 0 {
		return nil, invalid(fmt.Errorf("avatar lookup needs at least one name"))
	}

	var (
		wait     sync.WaitGroup
		mu       sync.Mutex
		found    []PersonAvatar
		firstErr error
		byOpen   = map[string]bool{}
	)
	for _, name := range wanted {
		wait.Add(1)
		go func(name string) {
			defer wait.Done()
			people, err := s.avatarsOf(ctx, name)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for _, person := range people {
				if byOpen[person.OpenID] {
					continue
				}
				byOpen[person.OpenID] = true
				found = append(found, person)
			}
		}(name)
	}
	wait.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return found, nil
}

func (s *ResolveService) avatarsOf(ctx context.Context, name string) ([]PersonAvatar, error) {
	s.mu.Lock()
	cached, ok := s.avatars[name]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}

	users, err := s.search.SearchUserAvatars(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolve avatar name=%q: %w", name, err)
	}
	people := make([]PersonAvatar, 0, len(users))
	for _, user := range users {
		url := user.Avatar.Medium
		if url == "" {
			url = user.Avatar.Small
		}
		if user.OpenID == "" || url == "" {
			continue
		}
		people = append(people, PersonAvatar{OpenID: user.OpenID, Name: user.Name, AvatarURL: url})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.avatars[name] = people
	if err := s.persistAvatars(); err != nil {
		return nil, err
	}
	return people, nil
}

// persistAvatars rewrites the whole cache file. The caller holds s.mu.
func (s *ResolveService) persistAvatars() error {
	raw, err := json.Marshal(s.avatars)
	if err != nil {
		return fmt.Errorf("encode avatar cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.avatarFile), 0o755); err != nil {
		return fmt.Errorf("create avatar cache dir: %w", err)
	}
	if err := os.WriteFile(s.avatarFile, raw, 0o644); err != nil {
		return fmt.Errorf("write avatar cache %q: %w", s.avatarFile, err)
	}
	return nil
}

// Resolve runs the lark-cli people search. fail-fast: a blank query is rejected
// as invalid input and any CLI failure surfaces unchanged (no silent fallback).
func (s *ResolveService) Resolve(ctx context.Context, query string) (*ResolveResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, invalid(fmt.Errorf("resolve query must not be blank"))
	}
	users, hasMore, err := s.search.SearchUser(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("resolve person query=%q: %w", query, err)
	}
	candidates := make([]ResolveCandidate, len(users))
	for i, user := range users {
		email := user.Email
		if email == "" {
			email = user.EnterpriseEmail
		}
		candidates[i] = ResolveCandidate{
			OpenID:     user.OpenID,
			Name:       user.LocalizedName,
			Email:      email,
			Department: user.Department,
			P2PChatID:  user.P2PChatID,
			IsExternal: user.IsCrossTenant,
			HasChatted: user.HasChatted,
		}
	}
	return &ResolveResult{Candidates: candidates, HasMore: hasMore}, nil
}
