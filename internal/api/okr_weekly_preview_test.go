package api

import (
	"context"
	"encoding/json"
	"os"
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
	"github.com/cloudwego/hertz/pkg/protocol"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type weeklyPreviewDocumentStub struct{}

func (weeklyPreviewDocumentStub) CreateMarkdownDocument(context.Context, string, string) (larkcli.MarkdownDocument, error) {
	return larkcli.MarkdownDocument{}, nil
}

type weeklyPreviewPromptStub struct{}

func (weeklyPreviewPromptStub) Content(context.Context, string) (string, error) {
	return "评审提示词", nil
}

// previewReviewServiceStub wires a real review service onto a CLI that is never
// invoked by these route tests; the routes only require it to exist.
func previewReviewServiceStub(t *testing.T, workspace *okrworkspace.Service) *okrreview.Service {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "review-cli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := okrreview.NewService(okrreview.Options{
		Workspace: workspace, Prompts: weeklyPreviewPromptStub{},
		Bin: bin, Model: "stub-model", Sandbox: "read-only", ReasoningEffort: "high",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestWeeklyPreviewRoutesRequireTemplateAndExposeVersionedScores(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.Migrate(db); err != nil {
		t.Fatal(err)
	}
	objective := domain.Objective{ID: "o-preview-route", Quarter: "2026-Q3", Title: "增长"}
	kr := domain.KR{ID: "kr-preview-route", ObjectiveID: objective.ID, Title: "一级 KR"}
	point := domain.KRPoint{ID: "point-preview-route", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "策略 KR"}
	for _, row := range []any{&objective, &kr, &point} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := okrworkspace.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{Enabled: true}, unreachableOKRAuthProvider{}, okrAuthTestTokenStore(t))
	if err != nil {
		t.Fatal(err)
	}
	images, err := okrworkspace.NewImageStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	if err := RegisterOKRModuleRoutes(h, OKRModuleDependencies{
		Workspace: workspace, Images: images,
		Enabled: func(context.Context) (bool, error) { return true, nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterBizOKRModuleRoutes(h, BizOKRModuleDependencies{
		Workspace: workspace, Identity: identity, Documents: weeklyPreviewDocumentStub{},
		People:        newTestOKRPeopleResolver(t, &stubOKRPeopleSearcher{}),
		Enabled:       func(context.Context) (bool, error) { return true, nil },
		PreviewReview: previewReviewServiceStub(t, workspace),
	}); err != nil {
		t.Fatal(err)
	}

	request := func(method, path, body string) *protocol.Response {
		t.Helper()
		return ut.PerformRequest(h.Engine, method, path, &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
	}
	if response := request("POST", "/api/okr/weeks", `{"quarter":"2026-Q3","week":"2026-W37"}`); response.StatusCode() != 400 {
		t.Fatalf("missing template status=%d body=%s", response.StatusCode(), response.Body())
	}
	if response := request("POST", "/api/biz-okr/comments", `{}`); response.StatusCode() != 401 {
		t.Fatalf("anonymous comment status=%d body=%s", response.StatusCode(), response.Body())
	}
	previewBody := `{"quarter":"2026-Q3","week":"2026-W37","template_key":"okr_weekly_preview_v1"}`
	if response := request("POST", "/api/okr/weeks", previewBody); response.StatusCode() != 201 {
		t.Fatalf("open preview status=%d body=%s", response.StatusCode(), response.Body())
	}
	classicBody := `{"quarter":"2026-Q3","week":"2026-W37","template_key":"classic"}`
	if response := request("POST", "/api/okr/weeks", classicBody); response.StatusCode() != 409 {
		t.Fatalf("template conflict status=%d body=%s", response.StatusCode(), response.Body())
	}

	scoreBody := `{"quarter":"2026-Q3","week":"2026-W37","score":0.7,"expected_version":0}`
	response := request("PUT", "/api/biz-okr/scores/kr/"+kr.ID, scoreBody)
	if response.StatusCode() != 200 {
		t.Fatalf("score status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data okrworkspace.KRView `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Score == nil || payload.Data.Score.Value != 0.7 || payload.Data.Score.Version != 0 {
		t.Fatalf("score response = %+v", payload.Data.Score)
	}
	staleBody := `{"quarter":"2026-Q3","week":"2026-W37","score":0.4,"expected_version":1}`
	if response := request("PUT", "/api/biz-okr/scores/kr/"+kr.ID, staleBody); response.StatusCode() != 409 {
		t.Fatalf("score conflict status=%d body=%s", response.StatusCode(), response.Body())
	}
}
