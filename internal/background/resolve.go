package background

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/larkcli"
)

// userSearcher is the lark-cli subset this package needs. Declared here (not
// depending on the concrete client) so the resolver stays unit-testable.
type userSearcher interface {
	SearchUser(ctx context.Context, query string) ([]larkcli.UserCandidate, bool, error)
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

// ResolveService turns a name/email query into feishu open_id candidates so the
// person form never asks the user to type a raw ou_xxx id.
type ResolveService struct {
	search userSearcher
}

func NewResolveService(search userSearcher) (*ResolveService, error) {
	if search == nil {
		return nil, fmt.Errorf("resolve service searcher is nil")
	}
	return &ResolveService{search: search}, nil
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
