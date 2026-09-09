//go:build integration

package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
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

func TestQueryChatHistorySQLite(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "jarvis.db"),
	})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
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
		{MessageID: "m5", ChatID: "oc_hist", ChatMode: "group", SenderOpenID: "ou_a", SenderName: "Alice", SenderType: "user", MessageType: "text", Content: `字面量 100% a_b C:\tmp`, CreateTime: base + 3000},
		{MessageID: "m6", ChatID: "oc_hist", ChatMode: "group", SenderOpenID: "ou_a", SenderName: "Alice", SenderType: "user", MessageType: "text", Content: "通配对照 1000 axb", CreateTime: base + 4000},
	}
	if err := tx.Create(&seed).Error; err != nil {
		t.Fatalf("seed messages: %v", err)
	}
	tool := newHistoryTool(t, tx)

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
	for _, keyword := range []string{"100%", "a_b", `C:\tmp`} {
		out, err = tool.Invoke(context.Background(), json.RawMessage(`{"chat_id":"oc_hist","start_time":null,"end_time":null,"keyword":`+strconv.Quote(keyword)+`,"limit":10}`))
		if err != nil {
			t.Fatalf("Invoke(%q) error = %v", keyword, err)
		}
		if err := json.Unmarshal(out, &result); err != nil {
			t.Fatalf("decode %q result: %v", keyword, err)
		}
		if result.Count != 1 || result.Messages[0].MessageID != "m5" {
			t.Fatalf("literal keyword %q result = %#v", keyword, result)
		}
	}

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
