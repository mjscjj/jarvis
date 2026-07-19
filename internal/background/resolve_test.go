package background

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"jarvis/internal/larkcli"
)

type stubSearcher struct {
	users   []larkcli.UserCandidate
	hasMore bool
	err     error
	gotQ    string
}

func (s *stubSearcher) SearchUser(_ context.Context, query string) ([]larkcli.UserCandidate, bool, error) {
	s.gotQ = query
	return s.users, s.hasMore, s.err
}

func TestResolveServiceResolve(t *testing.T) {
	t.Run("maps candidates and falls back to enterprise email", func(t *testing.T) {
		stub := &stubSearcher{
			users: []larkcli.UserCandidate{{
				OpenID: "ou_abc", LocalizedName: "储节节", EnterpriseEmail: "c@x.com",
				Department: "公会", P2PChatID: "oc_1", IsCrossTenant: true, HasChatted: true,
			}},
			hasMore: true,
		}
		svc, err := NewResolveService(stub)
		if err != nil {
			t.Fatalf("NewResolveService() error = %v", err)
		}
		result, err := svc.Resolve(context.Background(), "储节节")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if stub.gotQ != "储节节" {
			t.Fatalf("Resolve() query = %q, want 储节节", stub.gotQ)
		}
		if !result.HasMore || len(result.Candidates) != 1 {
			t.Fatalf("Resolve() result = %+v, unexpected", result)
		}
		got := result.Candidates[0]
		if got.OpenID != "ou_abc" || got.Name != "储节节" || got.Email != "c@x.com" || !got.IsExternal {
			t.Fatalf("Resolve() candidate = %+v, unexpected", got)
		}
	})

	t.Run("blank query is invalid input", func(t *testing.T) {
		svc, _ := NewResolveService(&stubSearcher{})
		_, err := svc.Resolve(context.Background(), "  ")
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Resolve() error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("cli failure surfaces unchanged", func(t *testing.T) {
		svc, _ := NewResolveService(&stubSearcher{err: fmt.Errorf("boom")})
		_, err := svc.Resolve(context.Background(), "x")
		if err == nil || errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Resolve() error = %v, want non-invalid failure", err)
		}
	})
}
