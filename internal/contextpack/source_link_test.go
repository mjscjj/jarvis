package contextpack

import (
	"encoding/json"
	"testing"
)

func TestSourceURLUsesOnlyExplicitTriggerAndCapturedLink(t *testing.T) {
	const link = "https://applink.feishu.cn/client/chat/open?openChatId=oc_group&position=42"
	packet := map[string]any{
		"source": map[string]any{"trigger_message_id": "trigger", "source_message_ids": []string{"other", "trigger"}},
		"capture": map[string]any{"messages": []any{
			map[string]any{"message_id": "other", "source_url": "https://example.com/wrong"},
			map[string]any{"message_id": "trigger", "source_url": link},
		}},
		"annotation": map[string]any{"trigger_message_id": "other", "source_url": "https://example.com/invented"},
	}
	check := func(want string) {
		t.Helper()
		raw, err := json.Marshal(packet)
		if err != nil {
			t.Fatal(err)
		}
		if got := SourceURL(raw); got != want {
			t.Fatalf("SourceURL = %q, want %q", got, want)
		}
	}
	check(link)
	source := packet["source"].(map[string]any)
	source["trigger_message_id"] = "missing"
	check("")
	source["trigger_message_id"] = "trigger"
	source["source_message_ids"] = []string{"other"}
	check("")
	source["source_message_ids"] = []string{"trigger"}
	row := packet["capture"].(map[string]any)["messages"].([]any)[1].(map[string]any)
	for _, invalid := range []string{"", "javascript:alert(1)", "/local", "https:///missing-host"} {
		row["source_url"] = invalid
		check("")
	}
	row["source_url"] = link
	delete(source, "trigger_message_id") // Historical captures must not guess.
	check("")
	packet["source"] = "manual task"
	check("")
}
