package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/larkcli"
	"jarvis/internal/okrreview"
	"jarvis/internal/okrworkspace"
	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type browserDocumentStub struct{}

func (browserDocumentStub) CreateMarkdownDocument(_ context.Context, _ larkcli.UserCredentials, title, content string) (larkcli.MarkdownDocument, error) {
	return larkcli.MarkdownDocument{DocumentID: "regression-document", URL: "https://example.test/docx/regression"}, nil
}

func TestOKRBrowserWorkflow(t *testing.T) {
	if os.Getenv("OKR_BROWSER_URL") == "" {
		t.Skip("set OKR_BROWSER_URL to a Vite development server to run browser workflows")
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "okr.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := okrworkspace.Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&domain.Objective{ID: "official-o", Quarter: "2026-Q3", Title: "正式增长目标"},
		&domain.Objective{ID: "old-o", Quarter: "2026-Q2", Title: "历史季度目标"},
		&domain.KR{ID: "official-kr", ObjectiveID: "official-o", Title: "正式增长 KR", MetricNote: "季度正式口径"},
		&domain.KRMetric{ID: "official-m", KRID: "official-kr", Text: "正式目标 100", Light: domain.LightGreen},
		&domain.KRPoint{ID: "official-p", KRID: "official-kr", Kind: domain.PointKindStrategy, Title: "策略执行要点"},
		&domain.KRPoint{ID: "official-product", KRID: "official-kr", Kind: domain.PointKindProduct, Title: "产品交付要点", SortOrder: 1},
		&domain.KROwner{KRID: "official-kr", PersonID: 1, Name: "Regression Owner", Email: "owner@example.test"},
		&domain.KRTag{KRID: "official-kr", Type: domain.TagTypeBusinessCategory, Value: "增长"},
		&domain.KRTag{KRID: "official-kr", Type: domain.TagTypePriority, Value: "p1"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, quarter := range []string{"2026-Q3", "2026-Q4"} {
		plan, err := workspace.CreatePlan(t.Context(), okrworkspace.CreatePlanInput{Quarter: quarter, Title: quarter + " seeded Plan"})
		if err != nil {
			t.Fatal(err)
		}
		if quarter == "2026-Q4" {
			if _, err := workspace.CreatePlanObjective(t.Context(), plan.ID, okrworkspace.PlanObjectiveView{
				ID: "export-plan-o", Title: "导出目标", KRs: []okrworkspace.PlanKRView{
					{ID: "export-plan-kr-1", Title: "导出增长 KR", Tags: []okrworkspace.TagView{{Type: domain.TagTypeBusinessCategory, Value: "增长"}, {Type: domain.TagTypePriority, Value: "p0"}}, Points: []okrworkspace.PlanPointView{
						{ID: "export-plan-strategy-1", Kind: domain.PointKindStrategy, Title: "导出策略一"},
						{ID: "export-plan-strategy-2", Kind: domain.PointKindStrategy, Title: "导出策略二"},
						{ID: "export-plan-product-1", Kind: domain.PointKindProduct, Title: "导出产品一"},
					}},
					{ID: "export-plan-kr-2", Title: "导出第二 KR", Tags: []okrworkspace.TagView{{Type: domain.TagTypeBusinessCategory, Value: "增长"}, {Type: domain.TagTypePriority, Value: "p1"}}},
				},
			}, "browser-test"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for week, template := range map[string]domain.WeekTemplateKey{"2026-W34": domain.WeekTemplateClassic, "2026-W35": domain.WeekTemplateClassic, "2026-W36": domain.WeekTemplateOKRPreview} {
		if _, err := workspace.OpenWeek(t.Context(), okrworkspace.OpenWeekInput{Quarter: "2026-Q3", Week: week, TemplateKey: template}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	tokenStore := okrAuthTestTokenStore(t)
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, okrIdentityProviderStub{}, tokenStore)
	if err != nil {
		t.Fatal(err)
	}
	addOKRIdentitySession(t, db, "Jarvis", "on_833914b05fbbe2eb6623865af52d984f", "")
	images, err := okrworkspace.NewImageStore(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	activity, err := okrworkspace.NewActivityStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "review-cli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"## 隔离评审结果\\n补充量化验收标准。\"}}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	review, err := okrreview.NewService(okrreview.Options{Workspace: workspace, Prompts: weeklyPreviewPromptStub{}, Bin: bin, Model: "test", Sandbox: "read-only", ReasoningEffort: "high", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	people := newTestOKRPeopleResolver(t, &stubOKRPeopleSearcher{users: []larkcli.UserCandidate{{OpenID: "ou_regression", LocalizedName: "Regression Owner", EnterpriseEmail: "owner@example.test"}}})
	h := server.New(server.WithDisablePrintRoute(true))
	enabled := func(context.Context) (bool, error) { return true, nil }
	if err := RegisterOKRModuleRoutes(h, OKRModuleDependencies{Workspace: workspace, Images: images, Activity: activity, Identity: identity, Enabled: enabled}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterBizOKRModuleRoutes(h, BizOKRModuleDependencies{Workspace: workspace, Identity: identity, Activity: activity, Documents: browserDocumentStub{}, DocumentTokens: exportTokenStub{token: okrAuth.StoredToken{OpenID: "ou_Jarvis", AccessToken: "browser-test-token"}}, DocumentAppID: "cli_browser", People: people, PreviewReview: review, Enabled: enabled}); err != nil {
		t.Fatal(err)
	}
	h.GET("/api/people/search", SearchFeishuPeople(people))
	assets := http.StripPrefix("/okr-assets/", http.FileServer(http.Dir(images.Root())))
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/okr-assets/") {
			assets.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		headers := []ut.Header{{Key: "Cookie", Value: okrAuth.CookieName + "=Jarvis"}}
		for key, values := range r.Header {
			for _, value := range values {
				headers = append(headers, ut.Header{Key: key, Value: value})
			}
		}
		response := ut.PerformRequest(h.Engine, r.Method, r.URL.RequestURI(), &ut.Body{Body: bytes.NewReader(body), Len: len(body)}, headers...).Result()
		response.Header.VisitAll(func(key, value []byte) { w.Header().Add(string(key), string(value)) })
		w.WriteHeader(response.StatusCode())
		_, _ = w.Write(response.Body())
	}))
	defer backend.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "../../web/test/okrWorkflow.browser.mjs")
	command.Env = append(os.Environ(), "OKR_TEST_API="+backend.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("OKR browser regression: %v", err)
	}
}
