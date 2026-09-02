package background

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"jarvis/internal/larkcli"
)

type stubSearcher struct {
	users   []larkcli.UserCandidate
	hasMore bool
	err     error
	gotQ    string

	mu          sync.Mutex
	avatars     map[string][]larkcli.UserAvatar
	avatarErr   error
	avatarCalls []string
}

func (s *stubSearcher) SearchUser(_ context.Context, query string) ([]larkcli.UserCandidate, bool, error) {
	s.gotQ = query
	return s.users, s.hasMore, s.err
}

func (s *stubSearcher) SearchUserAvatars(_ context.Context, query string) ([]larkcli.UserAvatar, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.avatarCalls = append(s.avatarCalls, query)
	if s.avatarErr != nil {
		return nil, s.avatarErr
	}
	return s.avatars[query], nil
}

func avatarHit(openID, name, medium string) larkcli.UserAvatar {
	user := larkcli.UserAvatar{OpenID: openID, Name: name}
	user.Avatar.Medium = medium
	return user
}

func TestResolveServiceAvatars(t *testing.T) {
	t.Run("dedupes names and memoizes each lookup", func(t *testing.T) {
		stub := &stubSearcher{avatars: map[string][]larkcli.UserAvatar{
			"储节节": {avatarHit("ou_1", "储节节", "https://img/1.png")},
			"李鑫":  {avatarHit("ou_2", "李鑫", "https://img/2.png")},
		}}
		svc, _ := NewResolveService(stub, filepath.Join(t.TempDir(), "avatars.json"))

		people, err := svc.Avatars(context.Background(), []string{"储节节", " 李鑫 ", "储节节", ""})
		if err != nil {
			t.Fatalf("Avatars() error = %v", err)
		}
		if len(people) != 2 {
			t.Fatalf("Avatars() people = %+v, want 2", people)
		}
		if _, err := svc.Avatars(context.Background(), []string{"储节节", "李鑫"}); err != nil {
			t.Fatalf("Avatars() second call error = %v", err)
		}
		if len(stub.avatarCalls) != 2 {
			t.Fatalf("Avatars() cli calls = %v, want one per distinct name", stub.avatarCalls)
		}
	})

	t.Run("cache survives a restart", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "avatars.json")
		hits := map[string][]larkcli.UserAvatar{"储节节": {avatarHit("ou_1", "储节节", "https://img/1.png")}}
		warm, _ := NewResolveService(&stubSearcher{avatars: hits}, file)
		if _, err := warm.Avatars(context.Background(), []string{"储节节"}); err != nil {
			t.Fatalf("Avatars() error = %v", err)
		}

		restarted := &stubSearcher{avatarErr: fmt.Errorf("lark-cli must not be called")}
		svc, err := NewResolveService(restarted, file)
		if err != nil {
			t.Fatalf("NewResolveService() error = %v", err)
		}
		people, err := svc.Avatars(context.Background(), []string{"储节节"})
		if err != nil {
			t.Fatalf("Avatars() after restart error = %v", err)
		}
		if len(people) != 1 || people[0].AvatarURL != "https://img/1.png" {
			t.Fatalf("Avatars() after restart = %+v", people)
		}
	})

	t.Run("unreadable cache fails fast", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "avatars.json")
		if err := os.WriteFile(file, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := NewResolveService(&stubSearcher{}, file); err == nil {
			t.Fatal("NewResolveService() error = nil, want parse failure")
		}
	})

	t.Run("skips hits without an avatar", func(t *testing.T) {
		stub := &stubSearcher{avatars: map[string][]larkcli.UserAvatar{
			"无头像": {avatarHit("ou_3", "无头像", "")},
		}}
		svc, _ := NewResolveService(stub, filepath.Join(t.TempDir(), "avatars.json"))
		people, err := svc.Avatars(context.Background(), []string{"无头像"})
		if err != nil {
			t.Fatalf("Avatars() error = %v", err)
		}
		if len(people) != 0 {
			t.Fatalf("Avatars() people = %+v, want none", people)
		}
	})

	t.Run("empty list is invalid input", func(t *testing.T) {
		svc, _ := NewResolveService(&stubSearcher{}, filepath.Join(t.TempDir(), "avatars.json"))
		if _, err := svc.Avatars(context.Background(), []string{"  "}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Avatars() error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("cli failure surfaces unchanged", func(t *testing.T) {
		svc, _ := NewResolveService(&stubSearcher{avatarErr: fmt.Errorf("token expired")}, filepath.Join(t.TempDir(), "avatars.json"))
		_, err := svc.Avatars(context.Background(), []string{"储节节"})
		if err == nil || errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Avatars() error = %v, want non-invalid failure", err)
		}
	})
}

func TestResolveServiceResolve(t *testing.T) {
	t.Run("maps candidates and falls back to enterprise email", func(t *testing.T) {
		stub := &stubSearcher{
			users: []larkcli.UserCandidate{{
				OpenID: "ou_abc", LocalizedName: "测试用户", EnterpriseEmail: "c@x.com",
				Department: "公会", P2PChatID: "oc_1", IsCrossTenant: true, HasChatted: true,
			}},
			hasMore: true,
		}
		svc, err := NewResolveService(stub, filepath.Join(t.TempDir(), "avatars.json"))
		if err != nil {
			t.Fatalf("NewResolveService() error = %v", err)
		}
		result, err := svc.Resolve(context.Background(), "测试用户")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if stub.gotQ != "测试用户" {
			t.Fatalf("Resolve() query = %q, want 测试用户", stub.gotQ)
		}
		if !result.HasMore || len(result.Candidates) != 1 {
			t.Fatalf("Resolve() result = %+v, unexpected", result)
		}
		got := result.Candidates[0]
		if got.OpenID != "ou_abc" || got.Name != "测试用户" || got.Email != "c@x.com" || !got.IsExternal {
			t.Fatalf("Resolve() candidate = %+v, unexpected", got)
		}
	})

	t.Run("blank query is invalid input", func(t *testing.T) {
		svc, _ := NewResolveService(&stubSearcher{}, filepath.Join(t.TempDir(), "avatars.json"))
		_, err := svc.Resolve(context.Background(), "  ")
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Resolve() error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("cli failure surfaces unchanged", func(t *testing.T) {
		svc, _ := NewResolveService(&stubSearcher{err: fmt.Errorf("boom")}, filepath.Join(t.TempDir(), "avatars.json"))
		_, err := svc.Resolve(context.Background(), "x")
		if err == nil || errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Resolve() error = %v, want non-invalid failure", err)
		}
	})
}
