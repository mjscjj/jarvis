package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/authn"
	"jarvis/internal/chat"
	chatdomain "jarvis/internal/domain"
	okrAuth "jarvis/internal/okrworkspace/auth"
	"jarvis/internal/okrworkspace/domain"
	"jarvis/internal/okrworkspace/moduleconfig"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type sharedChatPrompts struct{}

func (sharedChatPrompts) Content(context.Context, string) (string, error) {
	return "OKR test prompt", nil
}

func TestOKRVisitorsShareHistoryWithoutOpeningPrincipalChat(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.AuthSession{}); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(chatdomain.ChatModels()...); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	tokens, err := okrAuth.NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := okrAuth.NewService(db, moduleconfig.IdentityConfig{Enabled: true, SessionTTLHours: 24}, okrIdentityProviderStub{}, tokens)
	if err != nil {
		t.Fatal(err)
	}
	addOKRIdentitySession(t, db, "alice", "on_alice", "")
	addOKRIdentitySession(t, db, "bob", "on_bob", "")
	principal, err := authn.NewServiceWithRunner(openAuthTestDB(t), "bytedcli", time.Hour, true, []string{"owner"}, authRunner{run: func(string, []string) ([]byte, error) {
		t.Fatal("visitor request must not start principal login")
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := chat.NewService(chat.Options{
		AgentName: "Jarvis", Bin: "sh", Model: "test-model", Sandbox: "read-only",
		ReasoningEffort: "medium", Timeout: time.Second, DB: db,
		FilesRoot: filepath.Join(t.TempDir(), "files"), Prompts: sharedChatPrompts{},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := server.Default()
	h.Use(authn.BrowserMiddleware(principal))
	h.GET("/api/chat/sessions", func(_ context.Context, c *app.RequestContext) { c.SetStatusCode(consts.StatusOK) })
	registerChatRoutes(h, "/api/okr-chat", service, RequireOKRIdentity(identity))
	origin := ut.Header{Key: "Origin", Value: "https://okr.example"}
	if response := ut.PerformRequest(h.Engine, "GET", "/api/chat/sessions", nil, origin).Result(); response.StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("principal chat status = %d", response.StatusCode())
	}
	if response := ut.PerformRequest(h.Engine, "GET", "/api/okr-chat/sessions", nil, origin).Result(); response.StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("unsigned OKR chat status = %d", response.StatusCode())
	}
	body := `{"agent":"codex","model":"test-model","reasoning_effort":"medium","title":"Shared OKR"}`
	response := ut.PerformRequest(h.Engine, "POST", "/api/okr-chat/sessions", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, origin,
		ut.Header{Key: "Cookie", Value: okrAuth.CookieName + "=alice"}).Result()
	if response.StatusCode() != consts.StatusCreated {
		t.Fatalf("create shared session: status=%d body=%s", response.StatusCode(), response.Body())
	}
	var created struct{ Data chat.SessionView }
	if err := json.Unmarshal(response.Body(), &created); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&chatdomain.ChatMessage{ID: "cm_shared", SessionID: created.Data.ID, Role: "user", Text: "Shared history"}).Error; err != nil {
		t.Fatal(err)
	}
	attachment, err := service.SaveUpload(t.Context(), created.Data.ID, "okr.txt", "text/plain", 3, func(path string) error {
		return os.WriteFile(path, []byte("OKR"), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"alice", "bob"} {
		cookie := ut.Header{Key: "Cookie", Value: okrAuth.CookieName + "=" + token}
		response := ut.PerformRequest(h.Engine, "GET", "/api/okr-chat/sessions", nil, origin, cookie).Result()
		if response.StatusCode() != consts.StatusOK || !strings.Contains(string(response.Body()), created.Data.ID) {
			t.Fatalf("%s status = %d body = %s", token, response.StatusCode(), response.Body())
		}
		response = ut.PerformRequest(h.Engine, "GET", "/api/okr-chat/sessions/"+created.Data.ID, nil, origin, cookie).Result()
		var detail struct{ Data chat.SessionView }
		if err := json.Unmarshal(response.Body(), &detail); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode() != consts.StatusOK || len(detail.Data.Messages) != 1 || detail.Data.Messages[0].Text != "Shared history" || len(detail.Data.PendingAttachments) != 1 || detail.Data.PendingAttachments[0].ID != attachment.ID {
			t.Fatalf("%s did not receive shared history and attachments: %s", token, response.Body())
		}
		if response := ut.PerformRequest(h.Engine, "GET", "/api/chat/sessions", nil, origin, cookie).Result(); response.StatusCode() != consts.StatusUnauthorized {
			t.Fatalf("%s opened principal chat: status=%d", token, response.StatusCode())
		}
	}
}
