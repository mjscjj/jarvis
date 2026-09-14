package notice

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"
)

type fakeRunner struct {
	calls   [][]string
	send    string
	read    string
	sendErr error
	readErr error
}

func (f *fakeRunner) Run(_ context.Context, out any, args ...string) error {
	f.calls = append(f.calls, args)
	if args[1] == "+messages-send" {
		if f.send != "" {
			if err := json.Unmarshal([]byte(f.send), out); err != nil {
				return err
			}
		}
		return f.sendErr
	}
	if f.read != "" {
		if err := json.Unmarshal([]byte(f.read), out); err != nil {
			return err
		}
	}
	return f.readErr
}

func workingRunner() *fakeRunner {
	return &fakeRunner{
		send: `{"ok":true,"data":{"message_id":"om_sent","chat_id":"oc_principal"}}`,
		read: `{"ok":true,"data":{"messages":[{"message_id":"om_sent","chat_id":"oc_principal","msg_type":"interactive","deleted":false,"message_app_link":"https://applink.feishu.cn/message"}]}}`,
	}
}

func testService(t *testing.T, f *fakeRunner) *Service {
	t.Helper()
	dir := t.TempDir()
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(dir, "test.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	s, err := NewService(db, f, "ou_principal", filepath.Join(dir, "notices.jsonl"), "192.168.1.20:18800", "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func payload(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	m := map[string]any{"content": "编译、测试通过。\n待作者修复。", "idempotency_key": "notice-review-v1"}
	for k, v := range fields {
		m[k] = v
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRemoteNoticeDetails(t *testing.T) {
	f := workingRunner()
	local := testService(t, f)
	s, err := NewService(local.db, f, "ou_principal", local.auditPath, "127.0.0.1:18800", "https://jarvis.example.com:8443/")
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{Title: "review", ActionType: "agent_task", Status: "executing", Version: 1}
	if err := s.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(t.Context(), payload(t, map[string]any{"task_id": task.ID})); err != nil {
		t.Fatal(err)
	}
	for i, arg := range f.calls[0] {
		if arg == "--content" {
			card := f.calls[0][i+1]
			want := "[查看详情](https://jarvis.example.com:8443/#/work/task/"
			if runtime.GOOS == "darwin" {
				want = "[查看详情（本机访问）](http://127.0.0.1:18800/#/work/task/"
			}
			if !strings.Contains(card, want) {
				t.Fatal(card)
			}
			return
		}
	}
	t.Fatal("missing card content")
}

func TestCardOpenTypeAndOptionalSections(t *testing.T) {
	for _, typ := range []string{"Notice", "复审观察"} {
		raw := payload(t, map[string]any{"type": typ, "details": "长说明\n完整保留", "extra": map[string]any{"建议": "明天复审", "编号": json.Number("794196753520925")}, "links": []any{map[string]any{"label": "查看 CR", "url": "https://code.example/mr/139?a=1&b=2"}}})
		_, card, err := prepare(raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(card, &doc); err != nil {
			t.Fatal(err)
		}
		if doc["schema"] != "2.0" || doc["header"] != nil {
			t.Fatalf("wrong card: %s", card)
		}
		elements := doc["body"].(map[string]any)["elements"].([]any)
		if elements[0].(map[string]any)["content"] != typ || elements[0].(map[string]any)["text_size"] != "heading-4" ||
			elements[1].(map[string]any)["text_size"] != "normal" {
			t.Fatalf("expected readable title and body text: %s", card)
		}
		if elements[2].(map[string]any)["size"] != "medium" {
			t.Fatalf("link button must use a readable size: %s", card)
		}
		if len(elements) != 5 {
			t.Fatalf("elements = %d", len(elements))
		}
		for _, element := range elements[3:] {
			panel := element.(map[string]any)
			if panel["expanded"] != false {
				t.Fatal("details must start folded")
			}
			inner := panel["elements"].([]any)[0].(map[string]any)
			if inner["text_size"] != "normal" {
				t.Fatalf("folded detail text must use normal size: %s", card)
			}
		}
		if strings.Contains(string(card), "callback") || !strings.Contains(string(card), "794196753520925") {
			t.Fatalf("unexpected card: %s", card)
		}
	}
	in, card, err := prepare(payload(t, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if in.Type != "Notice" || strings.Contains(string(card), "collapsible_panel") {
		t.Fatalf("minimal card: %s", card)
	}
}

func TestInvalidInputNeverSends(t *testing.T) {
	for _, fields := range []map[string]any{
		{"type": ""}, {"content": " "}, {"content": 42}, {"idempotency_key": ""},
		{"links": []any{map[string]any{"label": "坏链接", "url": "javascript:alert(1)"}}},
		{"content": strings.Repeat("长", 31000)},
	} {
		f := workingRunner()
		s := testService(t, f)
		if _, err := s.Send(t.Context(), payload(t, fields)); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("error=%v", err)
		}
		if len(f.calls) != 0 {
			t.Fatal("invalid input caused send")
		}
	}
}

func TestDeliveryAuditsWithoutChangingTask(t *testing.T) {
	f := workingRunner()
	s := testService(t, f)
	task := domain.Task{Title: "review", ActionType: "agent_task", Status: "executing", Version: 3}
	if err := s.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	raw := payload(t, map[string]any{"task_id": task.ID, "unrecognized_context": map[string]any{"kept": true}})
	d, err := s.Send(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Verified || d.Effect["kind"] != "feishu_message" || d.URL != "https://applink.feishu.cn/message" {
		t.Fatalf("delivery=%+v", d)
	}
	args := strings.Join(f.calls[0], " ")
	for _, want := range []string{"--user-id ou_principal", "--as bot", "--idempotency-key notice-review-v1", "--msg-type interactive"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %s: %s", want, args)
		}
	}
	var after domain.Task
	if err := s.db.First(&after, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != task.Status || after.Version != task.Version {
		t.Fatal("notice changed task")
	}
	audit, err := os.ReadFile(s.auditPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(audit)), "\n")
	if len(lines) != 2 || !strings.Contains(string(audit), "unrecognized_context") || !strings.Contains(lines[1], `"verified":true`) {
		t.Fatalf("audit=%s", audit)
	}
	var events int64
	s.db.Model(&domain.TaskEvent{}).Where("task_id = ?", task.ID).Count(&events)
	if events != 0 {
		t.Fatal("notice must not consume task event versions")
	}
}

func TestFailedReadbackPreservesSentReceipt(t *testing.T) {
	f := workingRunner()
	f.readErr = errors.New("upstream unavailable")
	s := testService(t, f)
	d, err := s.Send(t.Context(), payload(t, nil))
	if err == nil || d.MessageID != "om_sent" || d.Verified || d.Effect != nil {
		t.Fatalf("delivery=%+v error=%v", d, err)
	}
	audit, _ := os.ReadFile(s.auditPath)
	if !strings.Contains(string(audit), "upstream unavailable") || !strings.Contains(string(audit), "om_sent") {
		t.Fatalf("missing error receipt: %s", audit)
	}
}

func TestPartialSendAndBadReadbacks(t *testing.T) {
	for _, which := range []string{"send_error", "missing_id", "wrong_chat", "deleted"} {
		t.Run(which, func(t *testing.T) {
			f := workingRunner()
			switch which {
			case "send_error":
				f.sendErr = errors.New("send interrupted")
			case "missing_id":
				f.send = `{"ok":true,"data":{}}`
			case "wrong_chat":
				f.read = strings.ReplaceAll(f.read, "oc_principal", "oc_other")
			case "deleted":
				f.read = strings.ReplaceAll(f.read, `"deleted":false`, `"deleted":true`)
			}
			s := testService(t, f)
			d, err := s.Send(t.Context(), payload(t, nil))
			if err == nil || d.Verified || d.Effect != nil {
				t.Fatalf("delivery=%+v err=%v", d, err)
			}
			if which == "send_error" && (d.MessageID != "om_sent" || len(f.calls) != 1) {
				t.Fatal("partial send receipt lost or send retried")
			}
		})
	}
}

func TestAuditFailureStopsSend(t *testing.T) {
	f := workingRunner()
	s := testService(t, f)
	s.auditPath = t.TempDir()
	if _, err := s.Send(t.Context(), payload(t, nil)); err == nil {
		t.Fatal("want audit failure")
	}
	if len(f.calls) != 0 {
		t.Fatal("send happened without audit")
	}
}

func TestTaskNoticeUsesRuntimeDetailsLink(t *testing.T) {
	for _, withTask := range []bool{false, true} {
		f := workingRunner()
		s := testService(t, f)
		fields := map[string]any{"links": []any{map[string]any{"label": "查看 CR", "url": "https://example.com/mr/139"}}}
		if withTask {
			task := domain.Task{Title: "review", ActionType: "agent_task", Status: "executing", Version: 3}
			if err := s.db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			fields["task_id"] = task.ID
		}
		if _, err := s.Send(t.Context(), payload(t, fields)); err != nil {
			t.Fatal(err)
		}
		card := ""
		for i, arg := range f.calls[0] {
			if arg == "--content" {
				card = f.calls[0][i+1]
			}
		}
		if !strings.Contains(card, "https://example.com/mr/139") {
			t.Fatal(card)
		}
		want := "[查看详情](http://192.168.1.20:18800/#/work/task/"
		if runtime.GOOS == "darwin" {
			want = "[查看详情（本机访问）](http://127.0.0.1:18800/#/work/task/"
		}
		hasDetails := strings.Contains(card, want)
		if hasDetails != withTask {
			t.Fatalf("withTask=%v card=%s", withTask, card)
		}
	}
}
