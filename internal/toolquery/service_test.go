package toolquery

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestServiceProgressivelyLoadsCapturedData(t *testing.T) {
	db := openTestDB(t)
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	messages := []domain.Message{
		{MessageID: "m1", ChatID: "chat-a", ChatMode: "group", SenderOpenID: "ou-a", SenderName: "Alice", SenderType: "user", MessageType: "text", Content: "project alpha started", CreateTime: base.UnixMilli(), Source: "poll"},
		{MessageID: "m2", ChatID: "chat-b", ChatMode: "group", SenderOpenID: "ou-b", SenderName: "Bob", SenderType: "user", MessageType: "text", Content: "unrelated", CreateTime: base.Add(time.Minute).UnixMilli(), Source: "poll"},
	}
	for i := range messages {
		if err := db.Create(&messages[i]).Error; err != nil {
			t.Fatalf("create message: %v", err)
		}
	}
	name, url, localPath, extracted := "design.md", "https://example.test/design", "/tmp/design.md", strings.Repeat("full text ", 50)
	messageID := "m1"
	resource := domain.Resource{
		ResourceType: "doc", Name: &name, URL: &url, SourceMessageID: &messageID,
		LocalPath: &localPath, ExtractedText: &extracted, Downloaded: true,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource: %v", err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	from, until := base.Add(-time.Second), base.Add(time.Second)
	gotMessages, err := service.ListMessages(t.Context(), MessageFilter{
		ChatID: "chat-a", Keyword: "alpha", From: &from, Until: &until, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotMessages) != 1 || gotMessages[0].MessageID != "m1" || gotMessages[0].Content != "project alpha started" {
		t.Fatalf("messages = %#v", gotMessages)
	}

	summaries, err := service.ListResources(t.Context(), ResourceFilter{ChatID: "chat-a", Keyword: "full text", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].ID != resource.ID {
		t.Fatalf("resource summaries = %#v", summaries)
	}

	detail, err := service.GetResource(t.Context(), resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.LocalPath == nil || *detail.LocalPath != localPath || detail.ExtractedText == nil || *detail.ExtractedText != extracted {
		t.Fatalf("resource detail = %#v", detail)
	}
}

func TestServiceRejectsUnboundedAndMissingResourceQueries(t *testing.T) {
	service, err := NewService(openTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListMessages(t.Context(), MessageFilter{Limit: 101}); err == nil {
		t.Fatal("ListMessages limit=101 unexpectedly succeeded")
	}
	if _, err := service.GetResource(t.Context(), 99); err != ErrNotFound {
		t.Fatalf("GetResource error = %v, want ErrNotFound", err)
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	ddl := []string{
		`CREATE TABLE message (
			id INTEGER PRIMARY KEY AUTOINCREMENT, message_id TEXT NOT NULL UNIQUE,
			chat_id TEXT NOT NULL, group_id INTEGER, chat_mode TEXT NOT NULL,
			sender_open_id TEXT NOT NULL, sender_name TEXT NOT NULL, sender_type TEXT NOT NULL,
			message_type TEXT NOT NULL, content TEXT NOT NULL, content_raw TEXT,
			mentions_json TEXT, reply_to TEXT, root_id TEXT, thread_id TEXT,
			create_time INTEGER NOT NULL, update_time INTEGER, source TEXT NOT NULL,
			render_ok INTEGER NOT NULL DEFAULT 1, created_at DATETIME
		)`,
		`CREATE TABLE resource (
			id INTEGER PRIMARY KEY AUTOINCREMENT, resource_type TEXT NOT NULL,
			file_key TEXT, minute_token TEXT, doc_token TEXT, url TEXT, name TEXT,
			mime_type TEXT, size_bytes INTEGER, source_message_id TEXT, group_id INTEGER,
			local_path TEXT, downloaded INTEGER NOT NULL DEFAULT 0, content_hash TEXT,
			extracted_text TEXT, created_at DATETIME, updated_at DATETIME
		)`,
	}
	for _, statement := range ddl {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test schema: %v", err)
		}
	}
	return db
}
