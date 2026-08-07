package capture

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/larkcli"
)

func TestNormalizeChatIDs(t *testing.T) {
	got, err := normalizeChatIDs([]string{" oc_one ", "oc_two"})
	if err != nil {
		t.Fatalf("normalizeChatIDs() error = %v", err)
	}
	if strings.Join(got, ",") != "oc_one,oc_two" {
		t.Fatalf("normalizeChatIDs() = %#v", got)
	}
	if _, err := normalizeChatIDs([]string{"oc_one", "oc_one"}); err == nil {
		t.Fatal("normalizeChatIDs() accepted duplicate chat_id")
	}
	if _, err := normalizeChatIDs([]string{"oc_one", " "}); err == nil {
		t.Fatal("normalizeChatIDs() accepted empty chat_id")
	}

	dynamic := make([]string, 21)
	for i := range dynamic {
		dynamic[i] = fmt.Sprintf("oc_%02d", i)
	}
	got, err = normalizeChatIDs(dynamic)
	if err != nil {
		t.Fatalf("normalizeChatIDs() rejected dynamic list larger than 20: %v", err)
	}
	if len(got) != len(dynamic) {
		t.Fatalf("normalizeChatIDs() length = %d, want %d", len(got), len(dynamic))
	}
}

func TestParseCLITime(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	got, err := parseCLITime("2026-07-19 10:25", location)
	if err != nil {
		t.Fatalf("parseCLITime() error = %v", err)
	}
	want := time.Date(2026, 7, 19, 10, 25, 0, 0, location).UnixMilli()
	if got != want {
		t.Fatalf("parseCLITime() = %d, want %d", got, want)
	}
	if _, err := parseCLITime("2026/07/19 10:25", location); err == nil {
		t.Fatal("parseCLITime() accepted an unobserved timestamp layout")
	}
}

func TestFlattenMessages(t *testing.T) {
	input := []CLIMessage{{
		MessageID: "root",
		ThreadID:  "thread",
		ThreadReplies: []CLIMessage{{
			MessageID: "reply-1",
			ThreadReplies: []CLIMessage{{
				MessageID: "reply-2",
			}},
		}},
	}}
	got := flattenMessages(input)
	if len(got) != 3 {
		t.Fatalf("flattenMessages() length = %d, want 3", len(got))
	}
	if got[1].RootID != "root" || got[1].ParentID != "root" || got[1].ThreadID != "thread" {
		t.Fatalf("first reply linkage = root:%q parent:%q thread:%q", got[1].RootID, got[1].ParentID, got[1].ThreadID)
	}
	if got[2].RootID != "root" || got[2].ParentID != "reply-1" || got[2].ThreadID != "thread" {
		t.Fatalf("nested reply linkage = root:%q parent:%q thread:%q", got[2].RootID, got[2].ParentID, got[2].ThreadID)
	}
}

func TestToDomainMessageSystemSender(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	svc := &Service{opts: Options{Location: location}}
	group := &domain.Group{ID: 1, ChatID: "oc_fixture", ChatMode: "group"}

	// 群系统消息（msg_type=system、无 sender，message_id 前缀仍是 om_）应落库为占位
	// sender，而不是报错。这正是 "测试用户 invited ... to the group" 这类系统消息的形状。
	sys, err := svc.toDomainMessage(group, CLIMessage{
		MessageID: "om_x100b6ae8", MessageType: "system", CreateTime: "2026-07-19 10:00",
		Content: "测试用户 invited local dev to the group.", Sender: CLISender{},
	})
	if err != nil {
		t.Fatalf("system message rejected: %v", err)
	}
	if sys.SenderOpenID != systemSenderOpenID || sys.SenderType != systemMessageType {
		t.Fatalf("system sender = open_id:%q type:%q", sys.SenderOpenID, sys.SenderType)
	}

	// 普通消息若 sender 为空，仍 fail-fast 暴露问题。
	if _, err := svc.toDomainMessage(group, CLIMessage{
		MessageID: "om_normal", MessageType: "text", CreateTime: "2026-07-19 10:00",
		Sender: CLISender{},
	}); err == nil {
		t.Fatal("normal message with empty sender should fail-fast")
	}
}

func TestExtractResourceRefs(t *testing.T) {
	content := `img_key:img_v3_demo img_key:img_v3_demo
[minutes](https://example.feishu.cn/minutes/obcnMinute123)
[doc](https://example.feishu.cn/docx/DocToken456)
[link](https://example.com/path)`
	got := extractResourceRefs(content)
	if len(got) != 4 {
		t.Fatalf("extractResourceRefs() length = %d, want 4: %#v", len(got), got)
	}
	if got[0].ResourceType != "image" || got[0].FileKey != "img_v3_demo" {
		t.Errorf("image ref = %#v", got[0])
	}
	if got[1].ResourceType != "minutes" || got[1].MinuteToken == nil || *got[1].MinuteToken != "obcnMinute123" {
		t.Errorf("minutes ref = %#v", got[1])
	}
	if got[2].ResourceType != "doc" || got[2].DocToken == nil || *got[2].DocToken != "DocToken456" {
		t.Errorf("doc ref = %#v", got[2])
	}
	if got[3].ResourceType != "link" || got[3].URL == nil || *got[3].URL != "https://example.com/path" {
		t.Errorf("link ref = %#v", got[3])
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: &larkcli.APIError{Subtype: "rate_limited"}, want: "lark_api_rate_limited"},
		{err: &larkcli.CommandError{Cause: errors.New("exit")}, want: "lark_cli_process"},
		{err: errors.New("plain"), want: "errors.errorString"},
	}
	for _, tt := range tests {
		if got := classifyError(tt.err); got != tt.want {
			t.Errorf("classifyError(%T) = %q, want %q", tt.err, got, tt.want)
		}
	}
}
