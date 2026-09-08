package api

import (
	"bytes"
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

func TestSearchFeishuPeopleUsesSharedResolverContract(t *testing.T) {
	searcher := &stubOKRPeopleSearcher{
		users: []larkcli.UserCandidate{{
			OpenID: "ou_1", LocalizedName: "李鑫", EnterpriseEmail: "lixin@example.com",
			Department: "国际直播-公会", IsCrossTenant: false, HasChatted: true,
		}},
		hasMore: true,
	}
	h := server.New()
	h.GET("/api/people/search", SearchFeishuPeople(newTestOKRPeopleResolver(t, searcher)))

	response := ut.PerformRequest(h.Engine, "GET", "/api/people/search?q=%E6%9D%8E%E9%91%AB", nil).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if searcher.query != "李鑫" {
		t.Fatalf("resolver query=%q, want 李鑫", searcher.query)
	}
	var payload struct {
		Data struct {
			Candidates []struct {
				OpenID     string `json:"open_id"`
				Name       string `json:"name"`
				Email      string `json:"email"`
				Department string `json:"department"`
				IsExternal bool   `json:"is_external"`
				HasChatted bool   `json:"has_chatted"`
			} `json:"candidates"`
			HasMore bool `json:"has_more"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Data.HasMore || len(payload.Data.Candidates) != 1 {
		t.Fatalf("data=%+v", payload.Data)
	}
	user := payload.Data.Candidates[0]
	if user.OpenID != "ou_1" || user.Name != "李鑫" || user.Email != "lixin@example.com" || user.Department != "国际直播-公会" || user.IsExternal || !user.HasChatted {
		t.Fatalf("user=%+v", user)
	}
}

func TestLegacyPeopleSearchAdaptersUseSharedResolver(t *testing.T) {
	for _, test := range []struct {
		name      string
		method    string
		path      string
		body      []byte
		resultKey string
	}{
		{name: "world person resolve", method: "POST", path: "/api/persons/resolve", body: []byte(`{"query":"李鑫"}`), resultKey: "candidates"},
		{name: "biz okr people search", method: "GET", path: "/api/biz-okr/people/search?q=%E6%9D%8E%E9%91%AB", resultKey: "users"},
	} {
		t.Run(test.name, func(t *testing.T) {
			searcher := &stubOKRPeopleSearcher{users: []larkcli.UserCandidate{{OpenID: "ou_1", LocalizedName: "李鑫"}}}
			resolver := newTestOKRPeopleResolver(t, searcher)
			h := server.New()
			if test.method == "POST" {
				h.POST("/api/persons/resolve", ResolvePerson(resolver))
			} else {
				h.GET("/api/biz-okr/people/search", SearchWorkspacePeople(resolver))
			}
			var body *ut.Body
			if len(test.body) > 0 {
				body = &ut.Body{Body: bytes.NewReader(test.body), Len: len(test.body)}
			}
			response := ut.PerformRequest(h.Engine, test.method, test.path, body).Result()
			if response.StatusCode() != 200 {
				t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
			}
			var payload struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(response.Body(), &payload); err != nil {
				t.Fatal(err)
			}
			if _, ok := payload.Data[test.resultKey]; !ok {
				t.Fatalf("data=%s, want key %q", response.Body(), test.resultKey)
			}
			if searcher.query != "李鑫" {
				t.Fatalf("resolver query=%q, want 李鑫", searcher.query)
			}
		})
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

func TestSearchFeishuPeopleFailsFast(t *testing.T) {
	for _, test := range []struct {
		name     string
		path     string
		searcher *stubOKRPeopleSearcher
		status   int
	}{
		{name: "blank query", path: "/api/people/search?q=", searcher: &stubOKRPeopleSearcher{}, status: 400},
		{name: "lark cli failure", path: "/api/people/search?q=x", searcher: &stubOKRPeopleSearcher{err: fmt.Errorf("permission denied")}, status: 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := server.New()
			h.GET("/api/people/search", SearchFeishuPeople(newTestOKRPeopleResolver(t, test.searcher)))
			response := ut.PerformRequest(h.Engine, "GET", test.path, nil).Result()
			if response.StatusCode() != test.status {
				t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
			}
		})
	}
}
