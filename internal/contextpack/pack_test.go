package contextpack

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFrozenCaptureViewsAndAnnotationIsolation(t *testing.T) {
	source := `{"source_message_ids":["link","ask"],"unknown":{"id":9007199254740993},"payload":"完整准入判断"}`
	capture := `{"messages":[{"message_id":"link","content":"原始 MR 链接"},{"message_id":"ask","content":"仔细 review 这个"},{"message_id":"later","content":"周边消息"}],"project":{"summary":"完整项目背景"},"future":{"value":1e400}}`
	notes := `{"brief":"检查 MR","scene":"提交者请求审查","source_message_ids":["later"],"extra":{"id":9007199254740993}}`
	raw, err := Freeze([]byte(source), []byte(capture), "default", []byte(notes))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "原始 MR 链接") != 1 {
		t.Fatal("original duplicated")
	}
	overview, err := Read(raw, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"原始 MR 链接", "仔细 review 这个", "提交者请求审查"} {
		if !strings.Contains(string(overview), want) {
			t.Fatalf("missing %s", want)
		}
	}
	for _, hidden := range []string{"完整项目背景", "周边消息", "完整准入判断", "1e400"} {
		if strings.Contains(string(overview), hidden) {
			t.Fatalf("eagerly loaded %s", hidden)
		}
	}
	original, err := Source(raw)
	if err != nil || string(original) != source {
		t.Fatalf("source changed: %s %v", original, err)
	}
	for _, test := range []struct{ section, id, want string }{
		{"conversation", "", "周边消息"}, {"background", "", "完整项目背景"}, {"project", "", "完整项目背景"},
		{"", "later", "周边消息"}, {"future", "", "1e400"}, {"annotation", "", "9007199254740993"}, {"full", "", "完整准入判断"},
	} {
		body, err := Read(raw, test.section, test.id)
		if err != nil || !strings.Contains(string(body), test.want) {
			t.Fatalf("read %v: %s %v", test, body, err)
		}
	}
	if string(SourceMessageIDs(raw)) != `["link","ask"]` {
		t.Fatal("annotation changed source identity")
	}
}

func TestMachineAnchorsValidatedAndDirectRequestPreserved(t *testing.T) {
	for _, test := range []struct{ source, capture string }{
		{`{"source_message_ids":["missing"]}`, `{"messages":[{"message_id":"present"}]}`},
		{`{"source_message_ids":42}`, `{}`},
		{`{}`, `{"messages":[{"message_id":"same"},{"message_id":"same"}]}`},
	} {
		if _, err := Freeze([]byte(test.source), []byte(test.capture), "", nil); err == nil {
			t.Fatalf("invalid anchors accepted: %v", test)
		}
	}
	long := strings.Repeat("原始证据", 10000)
	source, _ := json.Marshal(long)
	raw, err := Freeze(source, []byte(`{"future":{"x":1}}`), "检查长文", nil)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := Read(raw, "", "")
	if err != nil || !strings.Contains(string(overview), long) {
		t.Fatal("direct request truncated")
	}
	for _, args := range [][2]string{{"conversation", "id"}, {"invented", ""}, {"", "missing"}} {
		if _, err := Read(raw, args[0], args[1]); err == nil {
			t.Fatalf("accepted invalid read: %v", args)
		}
	}
}

func TestIncompleteArchiveReadableButNotExecutable(t *testing.T) {
	// Historical absence must remain visible, rather than fabricating evidence
	// or silently treating a missing original as an empty direct request.
	raw := []byte(`{"source":{"source_message_ids":["om_missing"]},"capture":{"messages":[]},"annotation":{"brief":"历史记录"}}`)
	full, err := Read(raw, "full", "")
	if err != nil || !strings.Contains(string(full), "om_missing") {
		t.Fatalf("archive unavailable: %s %v", full, err)
	}
	if _, err := Read(raw, "", ""); err == nil {
		t.Fatal("missing primary silently omitted")
	}
	if _, err := SourceMessages(raw); err == nil {
		t.Fatal("missing primary accepted for execution")
	}
}
