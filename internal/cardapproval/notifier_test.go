package cardapproval

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"jarvis/internal/execute"
)

func TestNotifierSendsVersionBoundApprovalCard(t *testing.T) {
	runner := &fakeLarkRunner{response: `{"data":{"message_id":"om_card_1"}}`}
	notifier, err := newNotifier(runner, "ou_principal", "0.0.0.0:18800", func() (net.IP, error) {
		return net.ParseIP("192.168.3.91"), nil
	})
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
	notifier, err := newNotifier(runner, "ou_principal", "0.0.0.0:18800", func() (net.IP, error) {
		return net.ParseIP("192.168.3.91"), nil
	})
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

func TestNewNotifierFailsWithoutServerAddress(t *testing.T) {
	_, err := NewNotifier(&fakeLarkRunner{}, "ou_principal", "")
	if err == nil || !strings.Contains(err.Error(), "server address") {
		t.Fatalf("NewNotifier() error = %v", err)
	}
}

func TestNotifierResolvesLANAddressForEveryCard(t *testing.T) {
	runner := &fakeLarkRunner{response: `{"data":{"message_id":"om_card_1"}}`}
	addresses := []net.IP{net.ParseIP("192.168.1.10"), net.ParseIP("192.168.1.11")}
	resolveCalls := 0
	notifier, err := newNotifier(runner, "ou_principal", "0.0.0.0:18800", func() (net.IP, error) {
		address := addresses[resolveCalls]
		resolveCalls++
		return address, nil
	})
	if err != nil {
		t.Fatalf("newNotifier() error = %v", err)
	}
	for index, want := range []string{
		"http://192.168.1.10:18800/#/work/task/17",
		"http://192.168.1.11:18800/#/work/task/17",
	} {
		delivery, err := notifier.SendApproval(context.Background(), execute.ApprovalNotification{
			TaskID: 17, RunID: 29, Version: int32(index + 1), Title: "发布方案",
			Action: "发送方案", Target: "项目群", Artifact: "正文",
		})
		if err != nil {
			t.Fatalf("SendApproval() error = %v", err)
		}
		if delivery.URL != want {
			t.Fatalf("delivery URL = %q, want %q", delivery.URL, want)
		}
	}
	if resolveCalls != 2 {
		t.Fatalf("LAN resolver calls = %d, want 2", resolveCalls)
	}
}

func TestNotifierFailsBeforeSendingWhenLANAddressCannotBeResolved(t *testing.T) {
	runner := &fakeLarkRunner{response: `{"data":{"message_id":"om_card_1"}}`}
	notifier, err := newNotifier(runner, "ou_principal", "0.0.0.0:18800", func() (net.IP, error) {
		return nil, errors.New("no default route")
	})
	if err != nil {
		t.Fatalf("newNotifier() error = %v", err)
	}
	_, err = notifier.SendApproval(context.Background(), execute.ApprovalNotification{
		TaskID: 17, RunID: 29, Version: 1, Title: "发布方案",
		Action: "发送方案", Target: "项目群", Artifact: "正文",
	})
	if err == nil || !strings.Contains(err.Error(), "resolve approval detail LAN IPv4") {
		t.Fatalf("SendApproval() error = %v", err)
	}
	if len(runner.args) != 0 {
		t.Fatalf("lark runner was called after LAN resolution failed: %v", runner.args)
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
