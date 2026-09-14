package api

import (
	"context"
	"encoding/json"
	"errors"
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

func TestOKRVisitorsSeeOnlyTheirOwnSessions(t *testing.T) {
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
		ReasoningEffort: "medium", Timeout: time.Second, DB: db, OwnerRequired: true,
		FilesRoot: filepath.Join(t.TempDir(), "files"), Prompts: sharedChatPrompts{},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := server.Default()
	h.Use(authn.BrowserMiddleware(principal))
	h.GET("/api/chat/sessions", func(_ context.Context, c *app.RequestContext) { c.SetStatusCode(consts.StatusOK) })
	registerChatRoutes(h, "/api/okr-chat", service, RequireOKRIdentity(identity), scopeOKRChat)
	origin := ut.Header{Key: "Origin", Value: "https://okr.example"}
	alice := ut.Header{Key: "Cookie", Value: okrAuth.CookieName + "=alice"}
	bob := ut.Header{Key: "Cookie", Value: okrAuth.CookieName + "=bob"}
	request := func(method, path, body string, cookie ut.Header) *ut.ResponseRecorder {
		t.Helper()
		var payload *ut.Body
		if body != "" {
			payload = &ut.Body{Body: strings.NewReader(body), Len: len(body)}
		}
		return ut.PerformRequest(h.Engine, method, path, payload, origin, cookie)
	}
	if response := ut.PerformRequest(h.Engine, "GET", "/api/chat/sessions", nil, origin).Result(); response.StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("principal chat status = %d", response.StatusCode())
	}
	if response := ut.PerformRequest(h.Engine, "GET", "/api/okr-chat/sessions", nil, origin).Result(); response.StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("unsigned OKR chat status = %d", response.StatusCode())
	}
	// Old shared sessions have no owner. Keep their rows, but do not expose them
	// to an arbitrary person after ownership is enabled.
	legacy := chatdomain.ChatSession{ID: "cs_legacy", Title: "Legacy", Agent: "codex", Model: "test-model", ReasoningEffort: "medium"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	create := `{"agent":"codex","model":"test-model","reasoning_effort":"medium","title":"Private OKR"}`
	response := request("POST", "/api/okr-chat/sessions", create, alice).Result()
	if response.StatusCode() != consts.StatusCreated {
		t.Fatalf("create alice session: status=%d body=%s", response.StatusCode(), response.Body())
	}
	var created struct{ Data chat.SessionView }
	if err := json.Unmarshal(response.Body(), &created); err != nil {
		t.Fatal(err)
	}
	var stored chatdomain.ChatSession
	if err := db.First(&stored, "id = ?", created.Data.ID).Error; err != nil || stored.OwnerID != "on_alice" {
		t.Fatalf("session owner = %q, error = %v", stored.OwnerID, err)
	}
	if err := db.Create(&chatdomain.ChatMessage{ID: "cm_private", SessionID: created.Data.ID, Role: "user", Text: "Private history"}).Error; err != nil {
		t.Fatal(err)
	}
	attachment, err := service.SaveUpload(chat.WithOwner(t.Context(), "on_alice"), created.Data.ID, "okr.txt", "text/plain", 3, func(path string) error {
		return os.WriteFile(path, []byte("OKR"), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, person := range []struct {
		name   string
		cookie ut.Header
		owns   bool
	}{{"alice", alice, true}, {"bob", bob, false}} {
		list := request("GET", "/api/okr-chat/sessions", "", person.cookie).Result()
		if list.StatusCode() != consts.StatusOK || strings.Contains(string(list.Body()), legacy.ID) || strings.Contains(string(list.Body()), created.Data.ID) != person.owns {
			t.Fatalf("%s list status=%d body=%s", person.name, list.StatusCode(), list.Body())
		}
		detail := request("GET", "/api/okr-chat/sessions/"+created.Data.ID, "", person.cookie).Result()
		if person.owns {
			var loaded struct{ Data chat.SessionView }
			if err := json.Unmarshal(detail.Body(), &loaded); err != nil {
				t.Fatal(err)
			}
			if detail.StatusCode() != consts.StatusOK || len(loaded.Data.Messages) != 1 || loaded.Data.Messages[0].Text != "Private history" || len(loaded.Data.PendingAttachments) != 1 || loaded.Data.PendingAttachments[0].ID != attachment.ID {
				t.Fatalf("alice detail status=%d body=%s", detail.StatusCode(), detail.Body())
			}
		} else if detail.StatusCode() != consts.StatusNotFound {
			t.Fatalf("bob detail status=%d body=%s", detail.StatusCode(), detail.Body())
		}
		if legacyDetail := request("GET", "/api/okr-chat/sessions/"+legacy.ID, "", person.cookie).Result(); legacyDetail.StatusCode() != consts.StatusNotFound {
			t.Fatalf("%s opened legacy session: status=%d", person.name, legacyDetail.StatusCode())
		}
		if principalChat := request("GET", "/api/chat/sessions", "", person.cookie).Result(); principalChat.StatusCode() != consts.StatusUnauthorized {
			t.Fatalf("%s opened principal chat: status=%d", person.name, principalChat.StatusCode())
		}
	}
	crossUser := []struct{ method, path, body string }{
		{"PATCH", "/api/okr-chat/sessions/" + created.Data.ID, `{"title":"stolen"}`},
		{"DELETE", "/api/okr-chat/sessions/" + created.Data.ID, ""},
		{"POST", "/api/okr-chat/sessions/" + created.Data.ID + "/cancel", ""},
		{"DELETE", "/api/okr-chat/sessions/" + created.Data.ID + "/attachments/" + attachment.ID, ""},
		{"GET", "/api/okr-chat/attachments/" + attachment.ID + "/content", ""},
		{"POST", "/api/okr-chat/sessions", `{"agent":"codex","model":"test-model","reasoning_effort":"medium","from_session_id":"` + created.Data.ID + `"}`},
	}
	for _, test := range crossUser {
		response := request(test.method, test.path, test.body, bob).Result()
		if response.StatusCode() != consts.StatusNotFound {
			t.Fatalf("bob %s %s status=%d body=%s", test.method, test.path, response.StatusCode(), response.Body())
		}
	}
	if err := service.StreamSession(chat.WithOwner(t.Context(), "on_bob"), created.Data.ID, chat.SendInput{Message: "steal history"}, func(chat.Event) error { return nil }); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("bob stream error=%v", err)
	}
	var messageCount int64
	if err := db.Model(&chatdomain.ChatMessage{}).Where("session_id = ?", created.Data.ID).Count(&messageCount).Error; err != nil || messageCount != 1 {
		t.Fatalf("cross-user stream wrote messages: count=%d error=%v", messageCount, err)
	}
	if _, err := service.SaveUpload(chat.WithOwner(t.Context(), "on_bob"), created.Data.ID, "denied.txt", "text/plain", 3, func(string) error {
		t.Fatal("cross-user upload must not write a file")
		return nil
	}); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("cross-user upload error=%v", err)
	}
	if bobSession := request("POST", "/api/okr-chat/sessions", create, bob).Result(); bobSession.StatusCode() != consts.StatusCreated {
		t.Fatalf("bob could not create own session: %d %s", bobSession.StatusCode(), bobSession.Body())
	}
	bobList := request("GET", "/api/okr-chat/sessions", "", bob).Result()
	if bobList.StatusCode() != consts.StatusOK || strings.Contains(string(bobList.Body()), created.Data.ID) || !strings.Contains(string(bobList.Body()), "Private OKR") {
		t.Fatalf("bob list after own session: status=%d body=%s", bobList.StatusCode(), bobList.Body())
	}
}
