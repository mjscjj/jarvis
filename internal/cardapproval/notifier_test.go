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
	notifier, err := newNotifier(runner, "小贾", "ou_principal", "0.0.0.0:18800", func() (net.IP, error) {
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
	notifier, err := newNotifier(runner, "小贾", "ou_principal", "0.0.0.0:18800", func() (net.IP, error) {
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

func TestNotifierShowsCompactFormOnlyWhenFollowupIsExplicit(t *testing.T) {
	without := approvalCard(execute.ApprovalNotification{TaskID: 1, Version: 1, Title: "t", Action: "a", Target: "b", Artifact: "c"}, "http://example.com", "小贾")
	withoutJSON, _ := json.Marshal(without)
	if strings.Contains(string(withoutJSON), `"tag":"form"`) || strings.Contains(string(withoutJSON), `"tag":"input"`) {
		t.Fatalf("card without followup unexpectedly has form: %s", withoutJSON)
	}
	with := approvalCard(execute.ApprovalNotification{
		TaskID: 1, Version: 1, Title: "t", Action: "a", Target: "b", Artifact: "c",
		NeedsFollowup: "请指定灰度范围",
	}, "http://example.com", "小贾")
	withJSON, _ := json.Marshal(with)
	for _, want := range []string{
		`"tag":"form"`, `"tag":"input"`, `"name":"approval_note"`,
		`"action_type":"form_submit"`,
		`"action":"jarvis_approval"`, `"decision":"approve"`, `"decision":"reject"`,
		"请指定灰度范围",
	} {
		if !strings.Contains(string(withJSON), want) {
			t.Fatalf("card with followup missing %q: %s", want, withJSON)
		}
	}
	body := with["body"].(map[string]any)
	form := body["elements"].([]any)[1].(map[string]any)
	formElements := form["elements"].([]any)
	// Feishu rejects the whole card with 300123 unless a submit button is a
	// direct child of the form container, so assert the exact position.
	for _, element := range formElements {
		if node, ok := element.(map[string]any); ok && node["tag"] == "column_set" {
			t.Fatalf("form buttons nested in a column_set are invisible to Feishu: %#v", form)
		}
	}
	for index, decision := range []string{"approve", "reject"} {
		button := formElements[2+index].(map[string]any)
		if button["action_type"] != "form_submit" {
			t.Fatalf("%s button action_type = %#v", decision, button["action_type"])
		}
		if strings.TrimSpace(button["name"].(string)) == "" {
			t.Fatalf("%s button inside a form has no name: %#v", decision, button)
		}
		// A submit button carrying behaviors also trips 300123: Feishu stops
		// treating it as a submit button, so the payload must live in value.
		if _, ok := button["behaviors"]; ok {
			t.Fatalf("%s submit button still carries behaviors: %#v", decision, button)
		}
		value := button["value"].(map[string]any)
		if value["action"] != "jarvis_approval" || value["decision"] != decision {
			t.Fatalf("%s submit button value = %#v", decision, value)
		}
	}
	// The detail link shares the form container, so it needs a name and a
	// non-submit action_type of its own or Feishu drops the form's data.
	detailsButton := formElements[4].(map[string]any)
	if detailsButton["action_type"] != "link" || strings.TrimSpace(detailsButton["name"].(string)) == "" {
		t.Fatalf("detail button inside form = %#v", detailsButton)
	}
}

// Without a followup there is no form container, so the decision buttons keep
// the ordinary card 2.0 callback transport instead of a form submission.
func TestNotifierKeepsCallbackBehaviorsOutsideForm(t *testing.T) {
	card := approvalCard(execute.ApprovalNotification{
		TaskID: 1, Version: 1, Title: "t", Action: "a", Target: "b", Artifact: "c",
	}, "http://example.com", "小贾")
	decisions := card["body"].(map[string]any)["elements"].([]any)[1].(map[string]any)
	for index, decision := range []string{"approve", "reject"} {
		column := decisions["columns"].([]any)[index].(map[string]any)
		button := column["elements"].([]any)[0].(map[string]any)
		if _, ok := button["action_type"]; ok {
			t.Fatalf("%s button outside a form must not declare action_type: %#v", decision, button)
		}
		callback := button["behaviors"].([]any)[0].(map[string]any)
		value := callback["value"].(map[string]any)
		if callback["type"] != "callback" || value["decision"] != decision {
			t.Fatalf("%s button callback = %#v", decision, callback)
		}
	}
}

func TestNewNotifierFailsWithoutServerAddress(t *testing.T) {
	_, err := NewNotifier(&fakeLarkRunner{}, "小贾", "ou_principal", "")
	if err == nil || !strings.Contains(err.Error(), "server address") {
		t.Fatalf("NewNotifier() error = %v", err)
	}
}

func TestNotifierResolvesLANAddressForEveryCard(t *testing.T) {
	runner := &fakeLarkRunner{response: `{"data":{"message_id":"om_card_1"}}`}
	addresses := []net.IP{net.ParseIP("192.168.1.10"), net.ParseIP("192.168.1.11")}
	resolveCalls := 0
	notifier, err := newNotifier(runner, "小贾", "ou_principal", "0.0.0.0:18800", func() (net.IP, error) {
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
	notifier, err := newNotifier(runner, "小贾", "ou_principal", "0.0.0.0:18800", func() (net.IP, error) {
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
