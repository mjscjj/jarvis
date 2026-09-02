package api

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"jarvis/internal/background"
	"jarvis/internal/larkcli"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type stubOKRPeopleSearcher struct {
	users     []larkcli.UserCandidate
	hasMore   bool
	err       error
	query     string
	avatars   map[string][]larkcli.UserAvatar
	avatarErr error
}

func (s *stubOKRPeopleSearcher) SearchUser(_ context.Context, query string) ([]larkcli.UserCandidate, bool, error) {
	s.query = query
	return s.users, s.hasMore, s.err
}

func (s *stubOKRPeopleSearcher) SearchUserAvatars(_ context.Context, query string) ([]larkcli.UserAvatar, error) {
	if s.avatarErr != nil {
		return nil, s.avatarErr
	}
	return s.avatars[query], nil
}

func newTestOKRPeopleResolver(t *testing.T, searcher *stubOKRPeopleSearcher) *background.ResolveService {
	t.Helper()
	resolver, err := background.NewResolveService(searcher, filepath.Join(t.TempDir(), "avatars.json"))
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func TestSearchWorkspacePeopleUsesFeishuResolver(t *testing.T) {
	searcher := &stubOKRPeopleSearcher{
		users: []larkcli.UserCandidate{{
			OpenID: "ou_1", LocalizedName: "李鑫", EnterpriseEmail: "lixin@example.com",
			Department: "国际直播-公会", IsCrossTenant: false, HasChatted: true,
		}},
		hasMore: true,
	}
	h := server.New()
	h.GET("/api/okr/people/search", SearchWorkspacePeople(newTestOKRPeopleResolver(t, searcher)))

	response := ut.PerformRequest(h.Engine, "GET", "/api/okr/people/search?q=%E6%9D%8E%E9%91%AB", nil).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if searcher.query != "李鑫" {
		t.Fatalf("resolver query=%q, want 李鑫", searcher.query)
	}
	var payload struct {
		Data struct {
			Users []struct {
				OpenID     string `json:"open_id"`
				Name       string `json:"name"`
				Email      string `json:"email"`
				Department string `json:"department"`
				IsExternal bool   `json:"is_external"`
				HasChatted bool   `json:"has_chatted"`
			} `json:"users"`
			HasMore bool `json:"has_more"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Data.HasMore || len(payload.Data.Users) != 1 {
		t.Fatalf("data=%+v", payload.Data)
	}
	user := payload.Data.Users[0]
	if user.OpenID != "ou_1" || user.Name != "李鑫" || user.Email != "lixin@example.com" || user.Department != "国际直播-公会" || user.IsExternal || !user.HasChatted {
		t.Fatalf("user=%+v", user)
	}
}

func TestGetWorkspacePeopleAvatars(t *testing.T) {
	hit := larkcli.UserAvatar{OpenID: "ou_1", Name: "李鑫"}
	hit.Avatar.Medium = "https://img.example/lixin.png"
	searcher := &stubOKRPeopleSearcher{avatars: map[string][]larkcli.UserAvatar{"李鑫": {hit}}}
	h := server.New()
	h.GET("/api/okr/people/avatars", GetWorkspacePeopleAvatars(newTestOKRPeopleResolver(t, searcher)))

	response := ut.PerformRequest(h.Engine, "GET", "/api/okr/people/avatars?names=%E6%9D%8E%E9%91%AB", nil).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data struct {
			People []struct {
				OpenID    string `json:"open_id"`
				Name      string `json:"name"`
				AvatarURL string `json:"avatar_url"`
			} `json:"people"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.People) != 1 {
		t.Fatalf("data=%+v", payload.Data)
	}
	person := payload.Data.People[0]
	if person.OpenID != "ou_1" || person.Name != "李鑫" || person.AvatarURL != "https://img.example/lixin.png" {
		t.Fatalf("person=%+v", person)
	}
}

func TestGetWorkspacePeopleAvatarsFailsFast(t *testing.T) {
	for _, test := range []struct {
		name     string
		path     string
		searcher *stubOKRPeopleSearcher
		status   int
	}{
		{name: "blank names", path: "/api/okr/people/avatars?names=%20,%20", searcher: &stubOKRPeopleSearcher{}, status: 400},
		{name: "lark cli failure", path: "/api/okr/people/avatars?names=x", searcher: &stubOKRPeopleSearcher{avatarErr: fmt.Errorf("token expired")}, status: 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := server.New()
			h.GET("/api/okr/people/avatars", GetWorkspacePeopleAvatars(newTestOKRPeopleResolver(t, test.searcher)))
			response := ut.PerformRequest(h.Engine, "GET", test.path, nil).Result()
			if response.StatusCode() != test.status {
				t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
			}
		})
	}
}

func TestSearchWorkspacePeopleFailsFast(t *testing.T) {
	for _, test := range []struct {
		name     string
		path     string
		searcher *stubOKRPeopleSearcher
		status   int
	}{
		{name: "blank query", path: "/api/okr/people/search?q=", searcher: &stubOKRPeopleSearcher{}, status: 400},
		{name: "lark cli failure", path: "/api/okr/people/search?q=x", searcher: &stubOKRPeopleSearcher{err: fmt.Errorf("permission denied")}, status: 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := server.New()
			h.GET("/api/okr/people/search", SearchWorkspacePeople(newTestOKRPeopleResolver(t, test.searcher)))
			response := ut.PerformRequest(h.Engine, "GET", test.path, nil).Result()
			if response.StatusCode() != test.status {
				t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
			}
		})
	}
}
