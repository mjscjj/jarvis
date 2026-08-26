package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/capture"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/progress"
	"jarvis/internal/store"

	"github.com/cloudwego/hertz/pkg/app/server"
)

func runOKRProductCLI(t *testing.T, apiBase string, args ...string) map[string]any {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-tools"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "bash", append([]string{script}, args...)...)
	command.Env = append(command.Environ(), "JARVIS_API_BASE="+apiBase)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("jarvis-tools %s: %v: %s", strings.Join(args, " "), err, output)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode jarvis-tools %s output: %v: %s", strings.Join(args, " "), err, output)
	}
	return result
}

func jsonID(t *testing.T, value map[string]any) uint64 {
	t.Helper()
	id, ok := value["id"].(float64)
	if !ok || id < 1 || id != float64(uint64(id)) {
		t.Fatalf("invalid id in %#v", value)
	}
	return uint64(id)
}

func startOKRProductServer(t *testing.T, h *server.Hertz, addr string) string {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- h.Run() }()
	t.Cleanup(func() {
		_ = h.Close()
		select {
		case err := <-errCh:
			if err != nil && !strings.Contains(err.Error(), "closed") {
				t.Logf("Hertz close: %v", err)
			}
		case <-time.After(time.Second):
		}
	})
	base := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(base + "/api/okr-evidence/unassociated?limit=1")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return base
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Hertz server did not become ready at %s", addr)
	return ""
}

func TestOKRProductPathThroughRealHTTPAndJarvisTools(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	owner := domain.Person{OpenID: "ou_product_owner", Name: "Product Owner", Role: "owner", PriorityWeight: 1, IsActive: true}
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
		Location: time.Local, PrincipalOpenID: owner.OpenID, SearchOverlap: time.Minute,
		ActivationContext: time.Hour, AutoRelatedP2PTopN: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	h := server.New(server.WithHostPorts(addr))
	h.POST("/api/okrs", CreateOKR(okrService))
	h.GET("/api/okrs/:okr_id", GetOKR(okrService))
	h.GET("/api/okrs/:okr_id/weekly-view", GetOKRWeeklyView(okrService))
	h.POST("/api/projects", CreateProject(projectService))
	h.POST("/api/key-matters", CreateKeyMatter(matterService))
	h.GET("/api/pages/:type/:id", GetPage(pageService))
	h.GET("/api/okr-evidence/unassociated", ListUnassociatedOKREvidence(pageService))
	h.POST("/api/okr-evidence/apply", ApplyOKREvidence(pageService))
	h.GET("/api/facts", ListFacts(progressService))
	h.POST("/api/clues", AppendClue(captureService))
	apiBase := startOKRProductServer(t, h, addr)

	okr := runOKRProductCLI(t, apiBase, "create-okr", "--payload", fmt.Sprintf(
		`{"title":"季度增长目标","cycle":"2026 Q3","status":"进行中","owner_person_id":%d}`, owner.ID,
	))
	okrID := jsonID(t, okr)
	project := runOKRProductCLI(t, apiBase, "create-project", "--payload", fmt.Sprintf(
		`{"code":"growth","name":"增长发布","role":"owner","status":"active","priority":1,"okr_id":%d}`, okrID,
	))
	projectID := jsonID(t, project)
	matter := runOKRProductCLI(t, apiBase, "create-key-matter", "--payload", fmt.Sprintf(
		`{"title":"完成灰度","status":"验证中","project_id":%d,"due_at":null}`, projectID,
	))
	matterID := jsonID(t, matter)

	now := time.Now()
	occurredAt := now.Format(time.RFC3339)
	clue := runOKRProductCLI(t, apiBase, "append-clue", "--source", "meego", "--external-id", "wi-product-r1",
		"--title", "Meego 工作项更新：灰度验证", "--content", fmt.Sprintf("所属 OKR：%d；状态：验证中", okrID), "--occurred-at", occurredAt)
	evidenceMessageID, _ := clue["message_id"].(string)
	if evidenceMessageID == "" {
		t.Fatalf("clue lacks message_id: %#v", clue)
	}
	queue := runOKRProductCLI(t, apiBase, "list-unassociated-okr-evidence", "--source", "meego", "--date", now.Format("2006-01-02"), "--anchor", "灰度验证", "--limit", "20")
	if count, _ := queue["count"].(float64); count != 1 {
		t.Fatalf("unassociated evidence queue = %#v, want one item", queue)
	}

	page := runOKRProductCLI(t, apiBase, "get-page", "--type", "key_matter", "--id", fmt.Sprint(matterID))
	updatedAt, _ := page["updated_at"].(string)
	if updatedAt == "" {
		t.Fatalf("key-matter page lacks updated_at: %#v", page)
	}
	payload, err := json.Marshal(map[string]any{
		"subject_type": "key_matter", "subject_id": matterID, "evidence_message_id": evidenceMessageID,
		"description": "Meego 灰度工作项进入验证阶段。", "occurred_at": occurredAt,
		"content": "当前结论：灰度工作项已进入验证阶段。", "if_unchanged_since": updatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	applied := runOKRProductCLI(t, apiBase, "apply-okr-evidence", "--payload", string(payload))
	if applied["fact"] == nil || applied["page"] == nil {
		t.Fatalf("applied evidence = %#v, want Fact and Page", applied)
	}
	queue = runOKRProductCLI(t, apiBase, "list-unassociated-okr-evidence", "--source", "meego", "--date", now.Format("2006-01-02"), "--anchor", "灰度验证", "--limit", "20")
	if count, _ := queue["count"].(float64); count != 0 {
		t.Fatalf("associated evidence remained queued: %#v", queue)
	}
	page = runOKRProductCLI(t, apiBase, "get-page", "--type", "key_matter", "--id", fmt.Sprint(matterID))
	if summary, _ := page["summary"].(string); summary == "" {
		t.Fatalf("updated key-matter page = %#v", page)
	}
	facts := runOKRProductCLI(t, apiBase, "list-facts", "--subject-type", "key_matter", "--subject-id", fmt.Sprint(matterID), "--date", now.Format("2006-01-02"), "--limit", "20")
	items, _ := facts["items"].([]any)
	var meegoFacts int
	for _, item := range items {
		fact, _ := item.(map[string]any)
		if fact["source_kind"] == "meego" && fact["source_id"] != nil {
			meegoFacts++
		}
	}
	if meegoFacts != 1 {
		t.Fatalf("key-matter facts = %#v, want exactly one source-traceable Meego fact", facts)
	}
	weekly := runOKRProductCLI(t, apiBase, "get-okr-weekly-view", "--id", fmt.Sprint(okrID), "--date", now.Format("2006-01-02"))
	if changeCount, _ := weekly["change_count"].(float64); changeCount < 4 {
		t.Fatalf("weekly view = %#v, want hierarchy creation plus evidence changes", weekly)
	}

	var taskCount int64
	if err := db.Model(&domain.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("product path created %d Tasks; OKR world-model evidence must not materialize execution units", taskCount)
	}
	if err := (okrFlowReadOnlyRunner{}).Run(context.Background(), nil, "external-check"); err == nil {
		t.Fatal("test external runner no longer fails closed")
	}
}
