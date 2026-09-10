package toolcatalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoticePrincipalCLIFilePayloadAndReceipt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notice.json")
	content := "测试 `literal` $(not-a-command)\n第二行"
	payload, _ := json.Marshal(map[string]any{"type": "自拟类型", "content": content, "idempotency_key": "test-notice"})
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/notices/principal" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Error(err)
		}
		if got["content"] != content || got["type"] != "自拟类型" || got["task_id"] != float64(42) {
			t.Errorf("payload=%s", raw)
		}
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_notice","verified":true,"effect":{"kind":"feishu_message"}}}`)
	}))
	defer server.Close()
	out, err := runJarvisTools(t, server.URL, []string{"JARVIS_TASK_ID=42"}, "notice-principal", "--payload-file", file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"message_id":"om_notice"`) || !strings.Contains(out, `"effect"`) {
		t.Fatalf("out=%s", out)
	}
}

func TestNoticePrincipalCLIPreservesErrorReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"code":500,"message":"readback failed","data":{"message_id":"om_sent","verified":false}}`)
	}))
	defer server.Close()
	_, err := runJarvisTools(t, server.URL, nil, "notice-principal", "--payload", `{"content":"content","idempotency_key":"test"}`)
	if err == nil || !strings.Contains(err.Error(), "om_sent") {
		t.Fatalf("missing receipt error=%v", err)
	}
}
