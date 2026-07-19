package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"

	"gorm.io/gorm"
)

func newHistoryTool(t *testing.T, db *gorm.DB) *QueryChatHistoryTool {
	t.Helper()
	tool, err := NewQueryChatHistoryTool(db, time.Second, 50, time.UTC)
	if err != nil {
		t.Fatalf("NewQueryChatHistoryTool() error = %v", err)
	}
	return tool
}

func TestQueryChatHistoryRejectsBadArgs(t *testing.T) {
	// db is only touched after argument validation, so a nil-ish tool with a
	// real (empty) db still exercises the validation branches deterministically.
	tool := &QueryChatHistoryTool{timeout: time.Second, maxLimit: 50, location: time.UTC}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"chat_id":"","start_time":null,"end_time":null,"keyword":null,"limit":10}`)); err == nil {
		t.Fatal("Invoke accepted blank chat_id")
	}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"chat_id":"oc_1","start_time":null,"end_time":null,"keyword":null,"limit":0}`)); err == nil {
		t.Fatal("Invoke accepted non-positive limit")
	}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"chat_id":"oc_1","start_time":"not-a-time","end_time":null,"keyword":null,"limit":10}`)); err == nil {
		t.Fatal("Invoke accepted malformed start_time")
	}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"chat_id":"oc_1","extra":1,"start_time":null,"end_time":null,"keyword":null,"limit":10}`)); err == nil {
		t.Fatal("Invoke accepted unknown argument field")
	}
}

// TestQueryChatHistoryMySQL validates time-window, keyword and limit filtering
// against MySQL. Requires an empty database in JARVIS_TOOLS_TEST_MYSQL_DSN.
func TestQueryChatHistoryMySQL(t *testing.T) {
	dsn := os.Getenv("JARVIS_TOOLS_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JARVIS_TOOLS_TEST_MYSQL_DSN is required for query_chat_history integration test")
	}
	db, err := store.OpenMySQL(context.Background(), config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: 60,
	})
	if err != nil {
		t.Fatalf("OpenMySQL() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	tx := db.Begin()
	t.Cleanup(func() { tx.Rollback() })

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	seed := []domain.Message{
		{MessageID: "m1", ChatID: "oc_hist", ChatMode: "group", SenderOpenID: "ou_a", SenderName: "Alice", SenderType: "user", MessageType: "text", Content: "修复 login 仓库的分支", CreateTime: base},
		{MessageID: "m2", ChatID: "oc_hist", ChatMode: "group", SenderOpenID: "ou_b", SenderName: "Bob", SenderType: "user", MessageType: "text", Content: "无关寒暄", CreateTime: base + 1000},
		{MessageID: "m3", ChatID: "oc_hist", ChatMode: "group", SenderOpenID: "ou_a", SenderName: "Alice", SenderType: "user", MessageType: "text", Content: "另一个 payment 仓库", CreateTime: base + 2000},
		{MessageID: "m4", ChatID: "oc_other", ChatMode: "group", SenderOpenID: "ou_a", SenderName: "Alice", SenderType: "user", MessageType: "text", Content: "别的群 login", CreateTime: base + 500},
	}
	if err := tx.Create(&seed).Error; err != nil {
		t.Fatalf("seed messages: %v", err)
	}
	tool := newHistoryTool(t, tx)

	// Only this chat, keyword filter.
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"chat_id":"oc_hist","start_time":null,"end_time":null,"keyword":"仓库","limit":10}`))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	var result queryChatHistoryResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Count != 2 || result.Messages[0].MessageID != "m1" || result.Messages[1].MessageID != "m3" {
		t.Fatalf("keyword filter result = %#v", result)
	}

	// Time window excludes m3.
	end := time.UnixMilli(base + 1500).UTC().Format(time.RFC3339)
	out, err = tool.Invoke(context.Background(), json.RawMessage(`{"chat_id":"oc_hist","start_time":null,"end_time":"`+end+`","keyword":null,"limit":10}`))
	if err != nil {
		t.Fatalf("Invoke() time window error = %v", err)
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Count != 2 {
		t.Fatalf("time window result = %#v", result)
	}

	// Limit cap.
	tool2, err := NewQueryChatHistoryTool(tx, time.Second, 1, time.UTC)
	if err != nil {
		t.Fatalf("NewQueryChatHistoryTool() error = %v", err)
	}
	out, err = tool2.Invoke(context.Background(), json.RawMessage(`{"chat_id":"oc_hist","start_time":null,"end_time":null,"keyword":null,"limit":10}`))
	if err != nil {
		t.Fatalf("Invoke() limit error = %v", err)
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("limit cap result = %#v", result)
	}
}
