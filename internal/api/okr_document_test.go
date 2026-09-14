package api

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/larkcli"
	"jarvis/internal/okrworkspace"
	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"
)

type exportCreatorStub struct {
	calls []larkcli.UserCredentials
	err   error
}

func (s *exportCreatorStub) CreateMarkdownDocument(_ context.Context, credentials larkcli.UserCredentials, title, content string) (larkcli.MarkdownDocument, error) {
	s.calls = append(s.calls, credentials)
	return larkcli.MarkdownDocument{DocumentID: "doc-user", URL: "https://example.test/docx/user", Warnings: []string{"table converted"}}, s.err
}

type exportRefreshProvider struct {
	okrIdentityProviderStub
	calls int
}

func (p *exportRefreshProvider) RefreshGrant(_ context.Context, refresh string) (okrAuth.Grant, error) {
	p.calls++
	if refresh != "refresh-b" {
		return okrAuth.Grant{}, fmt.Errorf("unexpected refresh token")
	}
	return okrAuth.Grant{User: okrAuth.User{OpenID: "ou_b"}, AccessToken: "token-b-refreshed", RefreshToken: "refresh-b-next", ExpiresIn: time.Hour}, nil
}

type exportTokenStub struct {
	err   error
	token okrAuth.StoredToken
}

func (s exportTokenStub) Ensure(context.Context, string) (okrAuth.StoredToken, error) {
	return s.token, s.err
}

func TestOKRDocumentRouteUsesSessionUserAndRefresh(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "okr.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = okrworkspace.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	store, err := okrAuth.NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &exportRefreshProvider{}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, provider, store)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := okrAuth.NewUserTokens(store, provider)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"a", "b", "missing"} {
		addOKRIdentitySession(t, db, user, "on_"+user, user+"@example.test")
	}
	if _, err = store.Save(okrAuth.Grant{User: okrAuth.User{OpenID: "ou_a"}, AccessToken: "token-a", ExpiresIn: time.Hour}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Save(okrAuth.Grant{User: okrAuth.User{OpenID: "ou_b"}, AccessToken: "token-b-old", RefreshToken: "refresh-b", ExpiresIn: time.Hour}, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	creator := &exportCreatorStub{}
	h := server.New(server.WithDisablePrintRoute(true))
	err = RegisterBizOKRModuleRoutes(h, BizOKRModuleDependencies{Workspace: workspace, Identity: identity, Documents: creator, DocumentTokens: tokens, DocumentAppID: "cli_login", People: newTestOKRPeopleResolver(t, &stubOKRPeopleSearcher{}), PreviewReview: previewReviewServiceStub(t, workspace), Enabled: func(context.Context) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"title":"Weekly","content":"# Progress"}`
	for _, tc := range []struct {
		cookie, body string
		status       int
	}{{"", body, 401}, {"invalid", body, 401}, {"missing", body, 401}, {"a", `{"title":"Weekly","content":"# Progress","open_id":"ou_b"}`, 400}, {"a", body, 201}, {"b", body, 201}, {"b", body, 201}} {
		response := ut.PerformRequest(h.Engine, "POST", "/api/biz-okr/feishu-documents", &ut.Body{Body: strings.NewReader(tc.body), Len: len(tc.body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Cookie", Value: okrAuth.CookieName + "=" + tc.cookie}).Result()
		if response.StatusCode() != tc.status {
			t.Fatalf("cookie=%s status=%d body=%s", tc.cookie, response.StatusCode(), response.Body())
		}
		if strings.Contains(string(response.Body()), "token-") || strings.Contains(string(response.Body()), "link_share_entity") {
			t.Fatal("response exposed credential or obsolete sharing field")
		}
	}
	if len(creator.calls) != 3 {
		t.Fatalf("create calls=%d", len(creator.calls))
	}
	for i, want := range []string{"token-a", "token-b-refreshed", "token-b-refreshed"} {
		if creator.calls[i].AppID != "cli_login" || creator.calls[i].AccessToken != want {
			t.Fatalf("call %d used wrong user credentials", i)
		}
	}
	if provider.calls != 1 {
		t.Fatalf("refresh calls=%d", provider.calls)
	}
	stored, _, err := store.Load("ou_b")
	if err != nil || stored.RefreshToken != "refresh-b-next" {
		t.Fatal("rotated grant not persisted")
	}
}

func TestOKRDocumentErrorsDoNotCreateAsAnotherUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "okr.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, okrIdentityProviderStub{}, okrAuthTestTokenStore(t))
	if err != nil {
		t.Fatal(err)
	}
	addOKRIdentitySession(t, db, "a", "on_a", "")
	good := okrAuth.StoredToken{OpenID: "ou_a", AccessToken: "secret-a"}
	for _, tc := range []struct {
		name          string
		tokens        OKRDocumentTokens
		apiErr        error
		status, calls int
		message       string
	}{
		{"unconfigured", nil, nil, 503, 0, "配置网页登录授权"},
		{"expired", exportTokenStub{err: okrAuth.ErrUserTokenUnusable}, nil, 401, 0, "重新登录"},
		{"refresh network", exportTokenStub{err: errors.New("provider detail")}, nil, 502, 0, "稍后重试"},
		{"wrong token owner", exportTokenStub{token: okrAuth.StoredToken{OpenID: "ou_b", AccessToken: "secret-b"}}, nil, 401, 0, "重新登录"},
		{"empty token", exportTokenStub{token: okrAuth.StoredToken{OpenID: "ou_a"}}, nil, 401, 0, "重新登录"},
		{"app scope", exportTokenStub{token: good}, &larkcli.APIError{Type: "authorization", Subtype: "app_scope_not_applied"}, 403, 1, "联系管理员"},
		{"user scope", exportTokenStub{token: good}, &larkcli.APIError{Type: "authorization", Subtype: "missing_scope"}, 403, 1, "重新登录"},
		{"token scope", exportTokenStub{token: good}, &larkcli.APIError{Type: "authorization", Subtype: "token_scope_insufficient"}, 403, 1, "重新登录"},
		{"revoked", exportTokenStub{token: good}, &larkcli.APIError{Type: "authentication", Subtype: "token_expired"}, 401, 1, "重新登录"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creator := &exportCreatorStub{err: tc.apiErr}
			h := server.New(server.WithDisablePrintRoute(true))
			h.POST("/export", RequireOKRIdentity(identity), CreateOKRDocument(creator, tc.tokens, "cli_login"))
			body := `{"title":"Weekly","content":"# Progress"}`
			response := ut.PerformRequest(h.Engine, "POST", "/export", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Content-Type", Value: "application/json"}, ut.Header{Key: "Cookie", Value: okrAuth.CookieName + "=a"}).Result()
			if response.StatusCode() != tc.status || !strings.Contains(string(response.Body()), tc.message) || len(creator.calls) != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", response.StatusCode(), len(creator.calls), response.Body())
			}
		})
	}
	// Local identity-disabled mode must never export with the server's account.
	disabled, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	creator := &exportCreatorStub{}
	h := server.New(server.WithDisablePrintRoute(true))
	h.POST("/export", RequireOKRIdentity(disabled), CreateOKRDocument(creator, exportTokenStub{token: good}, "cli_login"))
	response := ut.PerformRequest(h.Engine, "POST", "/export", nil).Result()
	if response.StatusCode() != 401 || len(creator.calls) != 0 {
		t.Fatal("synthetic Jarvis identity was allowed to export")
	}
}
