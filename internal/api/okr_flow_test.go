package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/capture"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/progress"
	"jarvis/internal/store"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type okrFlowReadOnlyRunner struct{}

func (okrFlowReadOnlyRunner) Run(context.Context, any, ...string) error {
	return fmt.Errorf("unexpected external CLI call in OKR flow test")
}

type okrFlowEnvelope[T any] struct {
	Code int    `json:"code"`
	Data T      `json:"data"`
	Msg  string `json:"msg"`
}

func performOKRFlowRequest[T any](t *testing.T, h *server.Hertz, method, path, body string) T {
	t.Helper()
	requestBody := &ut.Body{Body: bytes.NewBufferString(body), Len: len(body)}
	response := ut.PerformRequest(h.Engine, method, path, requestBody).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("%s %s status=%d body=%s", method, path, response.StatusCode(), response.Body())
	}
	var envelope okrFlowEnvelope[T]
	if err := json.Unmarshal(response.Body(), &envelope); err != nil {
		t.Fatalf("decode %s %s response: %v body=%s", method, path, err, response.Body())
	}
	if envelope.Code != 0 {
		t.Fatalf("%s %s code=%d msg=%s", method, path, envelope.Code, envelope.Msg)
	}
	return envelope.Data
}

func TestOKRHTTPFlowKeepsHierarchyProgressAndTaskBoundary(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	owner := domain.Person{OpenID: "ou_okr_owner", Name: "OKR Owner", Role: "owner", PriorityWeight: 1, IsActive: true}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}

	okrService, err := background.NewOKRService(db)
	if err != nil {
		t.Fatal(err)
	}
	projectService, err := background.NewProjectService(db)
	if err != nil {
		t.Fatal(err)
	}
	matterService, err := background.NewKeyMatterService(db)
	if err != nil {
		t.Fatal(err)
	}
	pageService, err := background.NewPageService(db)
	if err != nil {
		t.Fatal(err)
	}
	progressService, err := progress.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	captureService, err := capture.NewService(db, okrFlowReadOnlyRunner{}, capture.Options{
		PageSize: 50, ScanWorkers: 1, HotAge: time.Hour, WarmAge: 24 * time.Hour,
		Location: time.UTC, PrincipalOpenID: "ou_okr_owner", SearchOverlap: time.Minute,
		ActivationContext: time.Hour, AutoRelatedP2PTopN: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	h := server.New()
	h.POST("/api/okrs", CreateOKR(okrService))
	h.GET("/api/okrs/:okr_id", GetOKR(okrService))
	h.GET("/api/okrs/:okr_id/weekly-view", GetOKRWeeklyView(okrService))
	h.POST("/api/projects", CreateProject(projectService))
	h.POST("/api/key-matters", CreateKeyMatter(matterService))
	h.GET("/api/pages/:type/:id", GetPage(pageService))
	h.PUT("/api/pages/:type/:id", UpdatePage(pageService))
	h.POST("/api/facts", AppendFact(progressService))
	h.GET("/api/facts", ListFacts(progressService))
	h.POST("/api/clues", AppendClue(captureService))

	okr := performOKRFlowRequest[background.OKRView](t, h, "POST", "/api/okrs", fmt.Sprintf(
		`{"title":"季度增长目标","cycle":"2026 Q3","status":"进行中","owner_person_id":%d}`, owner.ID,
	))
	project := performOKRFlowRequest[background.ProjectView](t, h, "POST", "/api/projects", fmt.Sprintf(
		`{"code":"growth","name":"增长发布","role":"owner","status":"active","priority":1,"okr_id":%d}`, okr.ID,
	))
	matter := performOKRFlowRequest[background.KeyMatterView](t, h, "POST", "/api/key-matters", fmt.Sprintf(
		`{"title":"完成灰度","status":"验证中","project_id":%d,"due_at":null}`, project.ID,
	))

	hierarchy := performOKRFlowRequest[background.OKRView](t, h, "GET", fmt.Sprintf("/api/okrs/%d", okr.ID), "")
	if hierarchy.Owner == nil || hierarchy.Owner.ID != owner.ID || len(hierarchy.Projects) != 1 ||
		hierarchy.Projects[0].ID != project.ID || len(hierarchy.Projects[0].KeyMatters) != 1 ||
		hierarchy.Projects[0].KeyMatters[0].ID != matter.ID {
		t.Fatalf("OKR hierarchy = %#v", hierarchy)
	}

	page := performOKRFlowRequest[background.PageView](t, h, "GET", fmt.Sprintf("/api/pages/okr/%d", okr.ID), "")
	pageBody, err := json.Marshal(map[string]any{
		"content":            fmt.Sprintf("季度增长结果仍在验证，当前依赖 [增长发布](project:%d)。", project.ID),
		"if_unchanged_since": page.UpdatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedPage := performOKRFlowRequest[background.PageView](t, h, "PUT", fmt.Sprintf("/api/pages/okr/%d", okr.ID), string(pageBody))
	if updatedPage.Summary == "" || len(updatedPage.Outgoing) != 1 || updatedPage.Outgoing[0].ID != project.ID {
		t.Fatalf("updated OKR page = %#v", updatedPage)
	}

	clue := performOKRFlowRequest[capture.ClueResult](t, h, "POST", "/api/clues", fmt.Sprintf(
		`{"source":"meego","external_id":"wi-42-r2","title":"Meego 工作项更新：灰度验证","content":"所属 OKR：%d；状态：验证中；只读来源版本：r2","occurred_at":"2026-08-27T02:00:00Z"}`, okr.ID,
	))
	if !clue.Inserted || clue.ChatID != "clue:meego" {
		t.Fatalf("Meego clue = %#v, want one inserted read-only evidence item", clue)
	}
	replayed := performOKRFlowRequest[capture.ClueResult](t, h, "POST", "/api/clues", fmt.Sprintf(
		`{"source":"meego","external_id":"wi-42-r2","title":"Meego 工作项更新：灰度验证","content":"所属 OKR：%d；状态：验证中；只读来源版本：r2","occurred_at":"2026-08-27T02:00:00Z"}`, okr.ID,
	))
	if replayed.Inserted || replayed.MessageID != clue.MessageID {
		t.Fatalf("Meego clue replay = %#v, want idempotent no-op for %s", replayed, clue.MessageID)
	}
	var clueMessage domain.Message
	if err := db.Where("message_id = ?", clue.MessageID).Take(&clueMessage).Error; err != nil {
		t.Fatalf("load Meego clue message: %v", err)
	}
	if clueMessage.ChatMode != capture.ClueChatMode || clueMessage.Source != "clue" {
		t.Fatalf("Meego clue message = %#v, want neutral extractable evidence", clueMessage)
	}
	matterPage := performOKRFlowRequest[background.PageView](t, h, "GET", fmt.Sprintf("/api/pages/key_matter/%d", matter.ID), "")
	evidenceFactBody, err := json.Marshal(map[string]any{
		"subject_type": "key_matter", "subject_id": matter.ID,
		"description": "Meego 灰度工作项进入验证阶段。",
		"occurred_at": "2026-08-27T02:00:00Z", "source_kind": "meego", "source_id": clueMessage.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	evidenceFact := performOKRFlowRequest[progress.FactView](t, h, "POST", "/api/facts", string(evidenceFactBody))
	if evidenceFact.SourceKind == nil || *evidenceFact.SourceKind != "meego" || evidenceFact.SourceID == nil || *evidenceFact.SourceID != clueMessage.ID {
		t.Fatalf("generic evidence Fact = %#v, want source-traceable Fact", evidenceFact)
	}
	replayedEvidenceFact := performOKRFlowRequest[progress.FactView](t, h, "POST", "/api/facts", string(evidenceFactBody))
	if replayedEvidenceFact.ID != evidenceFact.ID {
		t.Fatalf("replayed generic evidence Fact = %#v, want idempotent Fact %#v", replayedEvidenceFact, evidenceFact)
	}
	matterPageBody, err := json.Marshal(map[string]any{
		"content": "当前结论：灰度工作项已进入验证阶段。", "if_unchanged_since": matterPage.UpdatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedMatterPage := performOKRFlowRequest[background.PageView](t, h, "PUT", fmt.Sprintf("/api/pages/key_matter/%d", matter.ID), string(matterPageBody))
	if updatedMatterPage.Summary == "" {
		t.Fatalf("updated key-matter Page = %#v", updatedMatterPage)
	}
	var associatedFactCount int64
	if err := db.Model(&domain.Fact{}).Where(
		"subject_type = ? AND subject_id = ? AND source_kind = ? AND source_id = ?",
		"key_matter", matter.ID, "meego", clueMessage.ID,
	).Count(&associatedFactCount).Error; err != nil {
		t.Fatalf("count associated evidence Facts: %v", err)
	}
	if associatedFactCount != 1 {
		t.Fatalf("associated evidence Fact count = %d, want 1 after replay", associatedFactCount)
	}
	fact := performOKRFlowRequest[progress.FactView](t, h, "POST", "/api/facts", fmt.Sprintf(
		`{"subject_type":"okr","subject_id":%d,"description":"Meego 灰度工作项进入验证阶段。","source_kind":"meego"}`, okr.ID,
	))
	if fact.SubjectType != "okr" || fact.SourceKind == nil || *fact.SourceKind != "meego" {
		t.Fatalf("OKR fact = %#v", fact)
	}
	facts := performOKRFlowRequest[struct {
		Items []progress.FactView `json:"items"`
	}](t, h, "GET", fmt.Sprintf("/api/facts?subject_type=okr&subject_id=%d", okr.ID), "")
	if len(facts.Items) < 2 {
		t.Fatalf("OKR facts = %#v, want create and Meego progress facts", facts.Items)
	}
	from := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	until := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	weekly := performOKRFlowRequest[background.OKRWeeklyView](t, h, "GET", fmt.Sprintf(
		"/api/okrs/%d/weekly-view?from=%s&until=%s", okr.ID, url.QueryEscape(from), url.QueryEscape(until),
	), "")
	if len(weekly.Items) != 3 || weekly.ChangeCount < 4 || weekly.Items[0].Summary == nil {
		t.Fatalf("OKR weekly view = %#v, want hierarchy, current Page conclusion and Fact changes", weekly)
	}

	var taskCount int64
	if err := db.Model(&domain.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("world-model updates materialized %d Tasks; Task must remain an independent execution unit", taskCount)
	}
}
