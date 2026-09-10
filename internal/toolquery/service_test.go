package toolquery

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestServiceProgressivelyLoadsCapturedData(t *testing.T) {
	db := openTestDB(t)
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	contentRaw := `{"text":"project alpha started"}`
	messages := []domain.Message{
		{MessageID: "m1", ChatID: "chat-a", ChatMode: "group", SenderOpenID: "ou-a", SenderName: "Alice", SenderType: "user", MessageType: "text", Content: "project alpha started", ContentRaw: &contentRaw, MentionsJSON: datatypes.JSON(`[{"id":"ou-owner","name":"Owner"}]`), CreateTime: base.UnixMilli(), Source: "poll"},
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
	if err := db.Exec(`INSERT INTO todo_event(id,todo_id,to_status,actor,detail,snapshot,created_at)
		VALUES (7,11,'extracted','m3','{"kind":"created"}','{"content":{"source":{}}}',?)`, base).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO task_event(id,task_id,task_version,event_type,to_status,actor_type,detail,occurred_at,created_at)
		VALUES (9,21,1,'execution_succeeded','done','m5','{"summary":"完成"}',?,?)`, base, base).Error; err != nil {
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
	if !strings.Contains(string(gotMessages[0].Mentions), `"id":"ou-owner"`) {
		t.Fatalf("message list mentions = %s", gotMessages[0].Mentions)
	}
	message, err := service.GetMessage(t.Context(), messages[0].ID)
	if err != nil || message.ID != messages[0].ID || message.Content != "project alpha started" || message.ContentRaw == nil || *message.ContentRaw != contentRaw {
		t.Fatalf("message = %#v, error = %v", message, err)
	}
	if !strings.Contains(string(message.Mentions), `"id":"ou-owner"`) {
		t.Fatalf("message detail mentions = %s", message.Mentions)
	}
	todoEvent, err := service.GetTodoEvent(t.Context(), 7)
	if err != nil || todoEvent.TodoID != 11 || !strings.Contains(string(todoEvent.Snapshot), `"source"`) {
		t.Fatalf("todo event = %#v, error = %v", todoEvent, err)
	}
	taskEvent, err := service.GetTaskEvent(t.Context(), 9)
	if err != nil || taskEvent.TaskID != 21 || !strings.Contains(string(taskEvent.Detail), "完成") {
		t.Fatalf("task event = %#v, error = %v", taskEvent, err)
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

func TestMessageIDBatchMatchesExactlyAndCannotTruncate(t *testing.T) {
	db := openTestDB(t)
	ids := []string{"clue:feishu_meeting:7683413135382417461", "clue:feishu_meeting:7683413135382417461:followup", "om_other"}
	for _, id := range ids {
		if err := db.Create(&domain.Message{
			MessageID: id, ChatID: "chat", ChatMode: "clue", SenderOpenID: "system",
			SenderName: "source", SenderType: "system", MessageType: "clue", Content: "same meeting ID in all bodies",
			CreateTime: 1, Source: "clue",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.ListMessages(t.Context(), MessageFilter{MessageIDs: []string{ids[0], "missing"}, Limit: 100})
	if err != nil || len(items) != 1 || items[0].MessageID != ids[0] {
		t.Fatalf("exact batch = %+v, error=%v", items, err)
	}
	for _, filter := range []MessageFilter{
		{MessageIDs: ids, Limit: 2},
		{MessageIDs: []string{""}, Limit: 100},
		{MessageIDs: []string{" padded"}, Limit: 100},
	} {
		if _, err := service.ListMessages(t.Context(), filter); err == nil {
			t.Fatalf("invalid or truncated batch accepted: %+v", filter)
		}
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
	for name, get := range map[string]func() error{
		"message":    func() error { _, err := service.GetMessage(t.Context(), 99); return err },
		"todo_event": func() error { _, err := service.GetTodoEvent(t.Context(), 99); return err },
		"task_event": func() error { _, err := service.GetTaskEvent(t.Context(), 99); return err },
	} {
		if err := get(); err != ErrNotFound {
			t.Fatalf("Get %s error = %v, want ErrNotFound", name, err)
		}
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
			message_type TEXT NOT NULL, content TEXT NOT NULL, source_url TEXT, content_raw TEXT,
			mentions_json TEXT, reply_to TEXT, root_id TEXT, thread_id TEXT,
			create_time INTEGER NOT NULL, update_time INTEGER, source TEXT NOT NULL,
			render_ok INTEGER NOT NULL DEFAULT 1,
			extraction_skipped INTEGER NOT NULL DEFAULT 0, created_at DATETIME
		)`,
		`CREATE TABLE resource (
			id INTEGER PRIMARY KEY AUTOINCREMENT, resource_type TEXT NOT NULL,
			file_key TEXT, minute_token TEXT, doc_token TEXT, url TEXT, name TEXT,
			mime_type TEXT, size_bytes INTEGER, source_message_id TEXT, group_id INTEGER,
			local_path TEXT, downloaded INTEGER NOT NULL DEFAULT 0, content_hash TEXT,
			extracted_text TEXT, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE todo_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, todo_id INTEGER NOT NULL,
			from_status TEXT, to_status TEXT NOT NULL, actor TEXT NOT NULL,
			detail JSON, snapshot JSON, created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE task_event (
			id INTEGER PRIMARY KEY AUTOINCREMENT, task_id INTEGER NOT NULL,
			task_version INTEGER NOT NULL, event_type TEXT NOT NULL, from_status TEXT,
			to_status TEXT NOT NULL, actor_type TEXT NOT NULL, actor_ref TEXT,
			run_id INTEGER, detail JSON, occurred_at DATETIME NOT NULL, created_at DATETIME NOT NULL
		)`,
	}
	for _, statement := range ddl {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test schema: %v", err)
		}
	}
	return db
}
