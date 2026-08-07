package cardapproval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"jarvis/internal/execute"
)

func TestNotifierSendsVersionBoundApprovalCard(t *testing.T) {
	runner := &fakeLarkRunner{response: `{"data":{"message_id":"om_card_1"}}`}
	notifier, err := NewNotifier(runner, "ou_principal", "http://192.168.3.91:18800")
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	delivery, err := notifier.SendApproval(context.Background(), execute.ApprovalNotification{
		TaskID: 17, RunID: 29, Version: 6, Title: "发布方案", Summary: "需要确认",
		Action: "发送方案", Target: "项目群", Artifact: "完整消息正文",
	})
	if err != nil {
		t.Fatalf("SendApproval() error = %v", err)
	}
	if delivery.MessageID != "om_card_1" {
		t.Fatalf("delivery = %#v", delivery)
	}
	if delivery.URL != "http://192.168.3.91:18800/#/work/task/17" {
		t.Fatalf("delivery URL = %q", delivery.URL)
	}
	joined := strings.Join(runner.args, "\n")
	for _, want := range []string{"--msg-type\ninteractive", "jarvis-approval-17-v6", `"task_id":17`, `"version":6`, `"decision":"approve"`, `"decision":"reject"`, `"default_url":"http://192.168.3.91:18800/#/work/task/17"`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("lark args missing %q:\n%s", want, joined)
		}
	}
}

func TestNotifierFailsWithoutMessageID(t *testing.T) {
	runner := &fakeLarkRunner{response: `{"data":{}}`}
	notifier, err := NewNotifier(runner, "ou_principal", "http://192.168.3.91:18800")
	if err != nil {
		t.Fatalf("NewNotifier() error = %v", err)
	}
	_, err = notifier.SendApproval(context.Background(), execute.ApprovalNotification{
		TaskID: 17, RunID: 29, Version: 6, Title: "发布方案",
		Action: "发送方案", Target: "项目群", Artifact: "正文",
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("SendApproval() error = %v", err)
	}
}

func TestNewNotifierFailsWithoutDetailBaseURL(t *testing.T) {
	_, err := NewNotifier(&fakeLarkRunner{}, "ou_principal", "")
	if err == nil || !strings.Contains(err.Error(), "detail base URL") {
		t.Fatalf("NewNotifier() error = %v", err)
	}
}

type fakeLarkRunner struct {
	response string
	args     []string
}

func (f *fakeLarkRunner) Run(_ context.Context, out any, args ...string) error {
	f.args = append([]string(nil), args...)
	return json.Unmarshal([]byte(f.response), out)
}
