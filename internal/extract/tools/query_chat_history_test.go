package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

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
