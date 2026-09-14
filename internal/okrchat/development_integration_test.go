package okrchat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"jarvis/internal/chat"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/okrworkspace/moduleconfig"
	"jarvis/internal/store"
)

type recoveryPromptReader struct{}

func (recoveryPromptReader) Content(context.Context, string) (string, error) {
	return "你是 Emily。直接回答用户当前问题。", nil
}

// Explicit opt-in: one real model turn inside the running development instance.
// Does not write product data or send any Feishu message.
func TestDevelopmentModelCanInspectFullSource(t *testing.T) {
	root := os.Getenv("EMILY_DEVELOPMENT_CHAT_ROOT")
	if root == "" {
		t.Skip("set EMILY_DEVELOPMENT_CHAT_ROOT for a live runtime check")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	id := "cs_" + hex.EncodeToString(nonce[:])
	r := &DevelopmentRuntime{container: "emily-development", docker: docker, root: root, cfg: moduleconfig.ChatConfig{Model: "gpt-5.6-sol", ReasoningEffort: "high", TimeoutSeconds: 120}}
	defer r.DeleteSession(id)
	var reply strings.Builder
	prompt := `Use a shell command to check that /opt/jarvis/go.mod, /opt/jarvis/web/package.json and /opt/jarvis/data/okr/okr.db exist, while /opt/jarvis/var/jarvis-lixiaolin.db and /var/run/docker.sock do not. Do not read credentials, database contents or any private files. Do not modify files or call external tools other than this shell check. If all checks pass, reply only EMILY_DEVELOPMENT_OK.`
	err = r.Stream(t.Context(), chat.Request{SessionID: id}, prompt, func(event chat.Event) error {
		if event.Kind == chat.EventDelta {
			reply.WriteString(event.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.String(), "EMILY_DEVELOPMENT_OK") {
		t.Fatalf("unexpected reply: %s", reply.String())
	}
}

// A stale native ID must be distinguishable from an authentication or network
// failure so the durable Chat session can recover with a fresh native thread.
func TestDevelopmentRuntimeReportsUnavailableNativeThread(t *testing.T) {
	root := os.Getenv("EMILY_DEVELOPMENT_CHAT_ROOT")
	if root == "" {
		t.Skip("set EMILY_DEVELOPMENT_CHAT_ROOT for a live runtime check")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	id := "cs_" + hex.EncodeToString(nonce[:])
	r := &DevelopmentRuntime{container: "emily-development", docker: docker, root: root, cfg: moduleconfig.ChatConfig{Model: "gpt-5.6-sol", ReasoningEffort: "high", TimeoutSeconds: 120}}
	defer r.DeleteSession(id)
	err = r.Stream(t.Context(), chat.Request{SessionID: id, ThreadID: "00000000-0000-7000-8000-000000000000"}, "Reply OK only.", func(chat.Event) error { return nil })
	if !errors.Is(err, chat.ErrNativeThreadUnavailable) {
		t.Fatalf("stale native thread error = %v", err)
	}
}

func TestDevelopmentServiceRecoversUnavailableNativeThread(t *testing.T) {
	root := os.Getenv("EMILY_DEVELOPMENT_CHAT_ROOT")
	if root == "" {
		t.Skip("set EMILY_DEVELOPMENT_CHAT_ROOT for a live runtime check")
	}
	cfg := moduleconfig.ChatConfig{DevelopmentContainer: "emily-development", Model: "gpt-5.6-sol", ReasoningEffort: "high", TimeoutSeconds: 120}
	svc, closeDB, err := openDevelopment(t.Context(), cfg, root, "Emily", recoveryPromptReader{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	ownerCtx := chat.WithOwner(t.Context(), "integration-user")
	session, err := svc.CreateSession(ownerCtx, chat.CreateSessionInput{Agent: "codex", Model: cfg.Model, ReasoningEffort: cfg.ReasoningEffort})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.DeleteSession(chat.WithOwner(context.Background(), "integration-user"), session.ID) }()
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: root + "/chat.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)
	if err := db.Model(&domain.ChatSession{}).Where("id = ?", session.ID).Update("native_thread_id", "00000000-0000-7000-8000-000000000000").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.ChatMessage{ID: "old-" + session.ID, SessionID: session.ID, Role: "user", Text: "之前讨论过这个问题"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.StreamSession(ownerCtx, session.ID, chat.SendInput{Message: "只回答 RECOVERED，不调用工具。"}, func(chat.Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	view, err := svc.GetSession(ownerCtx, session.ID)
	if err != nil || len(view.Messages) != 3 || !strings.Contains(view.Messages[2].Text, "RECOVERED") {
		t.Fatalf("recovered reply = %+v, error = %v", view, err)
	}
	var persisted domain.ChatSession
	if err := db.First(&persisted, "id = ?", session.ID).Error; err != nil || persisted.NativeThreadID == nil || *persisted.NativeThreadID == "00000000-0000-7000-8000-000000000000" {
		t.Fatalf("native thread was not replaced: %+v, error = %v", persisted, err)
	}
}
