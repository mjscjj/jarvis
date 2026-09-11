package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newPersistentTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "chat.db")+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open chat test database: %v", err)
	}
	if err := db.AutoMigrate(domain.ChatModels()...); err != nil {
		t.Fatalf("migrate chat test database: %v", err)
	}
	svc := newTestService(t)
	svc.db = db
	svc.filesRoot = filepath.Join(t.TempDir(), "files")
	return svc
}

func TestSessionPersistenceSearchForkAndAttachments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newPersistentTestService(t)

	session, err := svc.CreateSession(ctx, CreateSessionInput{
		Agent: "codex", Model: "gpt-5.5", ReasoningEffort: "high",
		Sources: []Source{{Kind: "world", Label: "世界模型"}},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	attachment, err := svc.SaveUpload(ctx, session.ID, "notes.txt", "text/plain", 5, func(path string) error {
		return os.WriteFile(path, []byte("hello"), 0o600)
	})
	if err != nil {
		t.Fatalf("SaveUpload() error = %v", err)
	}
	loaded, err := svc.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(loaded.PendingAttachments) != 1 || loaded.PendingAttachments[0].ID != attachment.ID {
		t.Fatalf("pending attachments = %#v", loaded.PendingAttachments)
	}
	if err := svc.DeletePendingAttachment(ctx, session.ID, attachment.ID); err != nil {
		t.Fatalf("DeletePendingAttachment() error = %v", err)
	}
	if _, err := svc.GetAttachment(ctx, attachment.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted pending attachment error = %v, want ErrNotFound", err)
	}
	attachment, err = svc.SaveUpload(ctx, session.ID, "notes.txt", "text/plain", 5, func(path string) error {
		return os.WriteFile(path, []byte("hello"), 0o600)
	})
	if err != nil {
		t.Fatalf("SaveUpload(second) error = %v", err)
	}

	agent, model := "codex", "gpt-5.5"
	for _, message := range []domain.ChatMessage{
		{ID: "m1", SessionID: session.ID, Role: "user", Text: "调查发布失败"},
		{ID: "m2", SessionID: session.ID, Role: "assistant", Text: "已经定位原因", Agent: &agent, Model: &model},
	} {
		if err := svc.db.WithContext(ctx).Create(&message).Error; err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}
	if err := svc.db.WithContext(ctx).Model(&domain.ChatAttachment{}).Where("id = ?", attachment.ID).Update("message_id", "m1").Error; err != nil {
		t.Fatalf("link seeded attachment: %v", err)
	}
	matches, err := svc.ListSessions(ctx, "发布失败", false)
	if err != nil || len(matches) != 1 || matches[0].ID != session.ID {
		t.Fatalf("ListSessions() = %#v, %v", matches, err)
	}

	forked, err := svc.CreateSession(ctx, CreateSessionInput{
		Title: "换用 Cursor", Agent: "cursor", Model: "auto", ReasoningEffort: "medium", FromSessionID: session.ID,
	})
	if err != nil {
		t.Fatalf("CreateSession(from history) error = %v", err)
	}
	forked, err = svc.GetSession(ctx, forked.ID)
	if err != nil {
		t.Fatalf("GetSession(fork) error = %v", err)
	}
	if len(forked.Messages) != 2 || forked.Messages[0].Text != "调查发布失败" || forked.Messages[1].Text != "已经定位原因" {
		t.Fatalf("forked messages = %#v", forked.Messages)
	}
	if len(forked.Messages[0].Attachments) != 1 || forked.Messages[0].Attachments[0].ID == attachment.ID {
		t.Fatalf("forked attachment = %#v", forked.Messages[0].Attachments)
	}
	forkedAttachmentID := forked.Messages[0].Attachments[0].ID
	if forked.Agent != "cursor" || forked.Model != "auto" {
		t.Fatalf("forked runtime = %s/%s", forked.Agent, forked.Model)
	}

	archived := true
	if _, err := svc.UpdateSession(ctx, session.ID, UpdateSessionInput{Archived: &archived}); err != nil {
		t.Fatalf("archive session: %v", err)
	}
	active, err := svc.ListSessions(ctx, "", false)
	if err != nil || len(active) != 1 || active[0].ID != forked.ID {
		t.Fatalf("active sessions = %#v, %v", active, err)
	}

	record, err := svc.GetAttachment(ctx, attachment.ID)
	if err != nil {
		t.Fatalf("GetAttachment() error = %v", err)
	}
	if content, err := os.ReadFile(record.LocalPath); err != nil || string(content) != "hello" {
		t.Fatalf("saved attachment = %q, %v", content, err)
	}
	if err := svc.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := svc.GetAttachment(ctx, attachment.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attachment after session delete error = %v, want ErrNotFound", err)
	}
	if copied, err := svc.GetAttachment(ctx, forkedAttachmentID); err != nil {
		t.Fatalf("copied attachment after source delete: %v", err)
	} else if content, err := os.ReadFile(copied.LocalPath); err != nil || string(content) != "hello" {
		t.Fatalf("copied attachment content = %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(svc.filesRoot, session.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session files still exist: %v", err)
	}
}

func TestRunningSessionRejectsRuntimeChangesAndDeletion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newPersistentTestService(t)
	session, err := svc.CreateSession(ctx, CreateSessionInput{Agent: "codex", Model: "gpt-5.5", ReasoningEffort: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	svc.active[session.ID] = func() {}

	model := "gpt-5.6"
	if _, err := svc.UpdateSession(ctx, session.ID, UpdateSessionInput{Model: &model}); !errors.Is(err, ErrConflict) {
		t.Fatalf("UpdateSession() error = %v, want ErrConflict", err)
	}
	if err := svc.DeleteSession(ctx, session.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("DeleteSession() error = %v, want ErrConflict", err)
	}
}

func TestCreateSessionRejectsMissingHistorySource(t *testing.T) {
	t.Parallel()
	svc := newPersistentTestService(t)
	_, err := svc.CreateSession(context.Background(), CreateSessionInput{Agent: "cursor", Model: "auto", ReasoningEffort: "medium", FromSessionID: "missing"})
	if err == nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateSession() error = %v, want ErrNotFound", err)
	}
}
