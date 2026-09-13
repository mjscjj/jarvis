package contextpack

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEvidenceSeparatesLegacyAdmissionFromPrivateConversation(t *testing.T) {
	capture := []byte(`{"group":{"chat_id":"private","chat_mode":"p2p","peer_name":"张若怡","summary":"OLD_WORLD_LOG"},"participants":[{"name":"张若怡"}],"messages":[{"message_id":"ask","sender_name":"我","content":"你现在能看到么"},{"message_id":"reply","sender_name":"张若怡","content":"可以的","reply_to":"ask"}]}`)
	for _, judgment := range []string{"Jarvis必须检查网页", "已经授权发消息"} {
		old, err := Freeze([]byte(`{"source_message_ids":["ask"],"payload":"`+judgment+`"}`), capture, judgment, []byte(`{"scene":"`+judgment+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		view, err := Evidence(old, "todo")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"张若怡", "你现在能看到么", "可以的", "p2p"} {
			if !strings.Contains(string(view), want) {
				t.Fatalf("missing %s: %s", want, view)
			}
		}
		if strings.Contains(string(view), judgment) || strings.Contains(string(view), "OLD_WORLD_LOG") {
			t.Fatal("admission or old world leaked")
		}
	}
	direct, err := Freeze([]byte(`{"instruction":"用户原始要求","payload":"用户自定义字段不能删除"}`), []byte(`{}`), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := Evidence(direct, "manual")
	if err != nil || !strings.Contains(string(view), "用户自定义字段不能删除") {
		t.Fatalf("manual source lost: %s %v", view, err)
	}
}
func TestEvidenceBudgetAndLosslessRange(t *testing.T) {
	original := strings.Repeat("长原文🙂", 12000)
	source, _ := json.Marshal(map[string]any{"instruction": original, "delivery_required": true, "reply_target": "群"})
	packet, err := FreezeEvidence(source, []byte(`{"messages":[]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := Evidence(packet, "manual")
	if err != nil || utf8.RuneCount(view) > EvidenceBudget {
		t.Fatalf("budget %v", err)
	}
	var recovered strings.Builder
	for offset := 0; offset < utf8.RuneCount(source); {
		part, err := Range(source, offset, 777)
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Text string
			Next int `json:"next_offset"`
		}
		if err := json.Unmarshal(part, &v); err != nil {
			t.Fatal(err)
		}
		recovered.WriteString(v.Text)
		offset = v.Next
	}
	if recovered.String() != string(source) {
		t.Fatal("ranged evidence changed")
	}
	if err := Validate([]byte(`{"format_version":99,"source":{},"capture":{},"annotation":{}}`)); err == nil {
		t.Fatal("unknown version accepted")
	}
}

func TestEvidenceKeepsOversizedTriggerAsReadableMetadata(t *testing.T) {
	content := strings.Repeat("触发原文", 12000)
	row, _ := json.Marshal(map[string]any{"message_id": "trigger", "sender_name": "同事", "content": content})
	capture, _ := json.Marshal(map[string]any{"messages": []json.RawMessage{row}})
	packet, err := FreezeEvidence([]byte(`{"source_message_ids":["trigger"]}`), capture, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := Evidence(packet, "todo")
	if err != nil {
		t.Fatal(err)
	}
	text := string(view)
	if utf8.RuneCount(view) > EvidenceBudget || !strings.Contains(text, `"message_id":"trigger"`) || !strings.Contains(text, `"body_omitted":true`) || !strings.Contains(text, `"content_characters":48000`) || strings.Contains(text, content) {
		t.Fatalf("oversized trigger is not a bounded readable anchor: %s", view)
	}
}
