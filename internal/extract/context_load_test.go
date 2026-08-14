package extract

import (
	"fmt"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestLoadContextMessagesKeepsThreadHistoryBeyondWindow pins the asymmetry
// between the two conversation units. A thread carries its own topic boundary,
// so a reply written a day after the discussion still needs that discussion as
// context — otherwise a demonstrative like "这个接入方案" reaches M5 with nothing
// to point at. Mainline messages have no such boundary and stay time-scoped.
func TestLoadContextMessagesKeepsThreadHistoryBeyondWindow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Message{}); err != nil {
		t.Fatal(err)
	}
	store := &PipelineStore{db: db}
	const chatID = "oc_ctx"
	const threadID = "omt_thread"
	now := time.Date(2026, 8, 14, 15, 53, 0, 0, time.UTC)

	insert := func(messageID string, at time.Time, thread, root string) domain.Message {
		row := domain.Message{
			MessageID: messageID, ChatID: chatID, ChatMode: "group",
			SenderOpenID: "ou_sender", SenderName: "储节节", SenderType: "user",
			MessageType: "text", Content: "内容 " + messageID,
			CreateTime: at.UnixMilli(), Source: "poll", RenderOK: true,
		}
		if thread != "" {
			row.ThreadID = &thread
		}
		if root != "" {
			row.RootID = &root
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("create message %s: %v", messageID, err)
		}
		return row
	}

	// Yesterday's thread discussion: what the demonstrative actually refers to.
	// om_thread_reply carries a root_id because it was captured through
	// chat-messages-list, which backfills it; the rest came from messages-search
	// and have none. Both must still land in the same unit.
	insert("om_thread_root", now.Add(-25*time.Hour), threadID, "")
	insert("om_thread_reply", now.Add(-25*time.Hour), threadID, "om_thread_root")
	// Same age, but mainline: genuinely stale, must stay out.
	insert("om_chat_old", now.Add(-25*time.Hour), "", "")
	insert("om_chat_recent", now.Add(-time.Hour), "", "")
	threadNew := insert("om_thread_new", now, threadID, "")
	chatNew := insert("om_chat_new", now, "", "")

	opts := LoadOptions{ContextMessages: 20, ContextWindow: 2 * time.Hour}

	if key := conversationKey(messageContext(&threadNew, true)); key != "topic:"+threadID {
		t.Fatalf("conversationKey(thread reply) = %q, want topic:%s", key, threadID)
	}
	threadContext, err := store.loadContextMessages(
		t.Context(), chatID, "topic:"+threadID, messageContext(&threadNew, true), opts,
	)
	if err != nil {
		t.Fatalf("loadContextMessages(thread) error = %v", err)
	}
	if len(threadContext) != 2 ||
		threadContext[0].MessageID != "om_thread_root" ||
		threadContext[1].MessageID != "om_thread_reply" {
		t.Fatalf("thread context = %s, want [om_thread_root om_thread_reply]", messageIDs(threadContext))
	}

	chatContext, err := store.loadContextMessages(
		t.Context(), chatID, "chat", messageContext(&chatNew, true), opts,
	)
	if err != nil {
		t.Fatalf("loadContextMessages(chat) error = %v", err)
	}
	if len(chatContext) != 1 || chatContext[0].MessageID != "om_chat_recent" {
		t.Fatalf("chat context = %s, want [om_chat_recent]", messageIDs(chatContext))
	}
}

func messageIDs(messages []MessageContext) []string {
	ids := make([]string, len(messages))
	for i := range messages {
		ids[i] = messages[i].MessageID
	}
	return ids
}
