package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"jarvis/internal/okrworkspace"
	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestOKRWriteIdentityRoutes(t *testing.T) {
	db := openAuthTestDB(t)
	if err := okrworkspace.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	tokens, err := okrAuth.NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, okrIdentityProviderStub{}, tokens)
	if err != nil {
		t.Fatal(err)
	}
	addOKRIdentitySession(t, db, "writer", "on_writer", "writer@example.test")
	addOKRIdentitySession(t, db, "expired", "on_expired", "expired@example.test")
	if err := db.Model(&domain.AuthSession{}).Where("name = ?", "expired").Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	images, err := okrworkspace.NewImageStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	enabled := func(context.Context) (bool, error) { return true, nil }
	h := server.New(server.WithDisablePrintRoute(true))
	// Exercise the actual route registrations behind a local reverse proxy.
	h.Use(func(ctx context.Context, c *app.RequestContext) {
		c.SetConn(authRequestContext("127.0.0.1", "").GetConn())
		c.Next(ctx)
	})
	if err := RegisterOKRModuleRoutes(h, OKRModuleDependencies{Workspace: workspace, Images: images, Identity: identity, Enabled: enabled}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterBizOKRModuleRoutes(h, BizOKRModuleDependencies{
		Workspace: workspace, Identity: identity, Documents: weeklyPreviewDocumentStub{},
		People: newTestOKRPeopleResolver(t, &stubOKRPeopleSearcher{}), PreviewReview: previewReviewServiceStub(t, workspace), Enabled: enabled,
	}); err != nil {
		t.Fatal(err)
	}
	for _, route := range h.Routes() {
		if route.Method == "GET" || strings.HasPrefix(route.Path, "/api/biz-okr/auth/") {
			continue
		}
		t.Run(route.Method+route.Path, func(t *testing.T) {
			response := ut.PerformRequest(h.Engine, route.Method, route.Path, nil, ut.Header{Key: "Origin", Value: "https://okr.example.test"}).Result()
			if response.StatusCode() != 401 {
				t.Fatalf("anonymous write: status=%d body=%s", response.StatusCode(), response.Body())
			}
		})
	}
	for _, tc := range []struct {
		name, cookie, forwarded, browser, actor string
		status                                  int
	}{
		{"signed in", "writer", "203.0.113.1", "cors", "ou_writer", 201},
		{"local CLI", "", "", "", "jarvis", 201},
		{"remote CLI", "", "203.0.113.1", "", "", 401},
		{"local browser", "", "", "cors", "", 401},
		{"invalid local cookie", "invalid", "", "", "", 401},
		{"expired local cookie", "expired", "", "", "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"quarter":"2026-Q3","title":"` + tc.name + `"}`
			response := ut.PerformRequest(h.Engine, "POST", "/api/biz-okr/plans", &ut.Body{Body: strings.NewReader(body), Len: len(body)},
				ut.Header{Key: "Cookie", Value: identity.CookieName() + "=" + tc.cookie},
				ut.Header{Key: "X-Forwarded-For", Value: tc.forwarded},
				ut.Header{Key: "Sec-Fetch-Mode", Value: tc.browser}).Result()
			if response.StatusCode() != tc.status {
				t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
			}
			if tc.status == 201 {
				var plan domain.OKRPlan
				if err := db.Where("title = ?", tc.name).First(&plan).Error; err != nil {
					t.Fatal(err)
				}
				if plan.CreatedBy != tc.actor {
					t.Fatalf("created_by=%q want %q", plan.CreatedBy, tc.actor)
				}
			}
		})
	}
	for _, path := range []string{"/api/biz-okr/me", "/api/biz-okr/plans?quarter=2026-Q3"} {
		if response := ut.PerformRequest(h.Engine, "GET", path, nil).Result(); response.StatusCode() != 200 {
			t.Fatalf("public read %s: %d", path, response.StatusCode())
		}
	}
	response := ut.PerformRequest(h.Engine, "POST", "/api/biz-okr/auth/feishu/device/missing/poll", nil, ut.Header{Key: "Sec-Fetch-Mode", Value: "cors"}).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("login poll blocked: %d", response.StatusCode())
	}
}
