package okrchat

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessOnlyForwardsExactOKRRoutes(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Forwarded-For") != "" {
			t.Error("caller credentials reached upstream")
		}
		if r.URL.RequestURI() != "/api/okr/board?period=2026-Q3" {
			t.Errorf("unexpected target %s", r.URL)
		}
		_, _ = io.WriteString(w, "OKR")
	}))
	defer upstream.Close()
	h, err := NewAccessHandler(upstream.URL, []string{"api.openai.com:443"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, target string
		status         int
	}{
		{"GET", "/api/okr/board?period=2026-Q3", 200},
		{"GET", "/api/chat/sessions", 403}, {"GET", "/api/okr-chat/sessions", 403},
		{"GET", "/api/tasks", 403}, {"GET", "/api/todos", 403}, {"GET", "/api/messages", 403},
		{"POST", "/api/biz-okr/preview-review", 403}, {"POST", "/api/biz-okr/comments", 403},
		{"POST", "/api/biz-okr/feishu-documents", 403}, {"GET", "/api/biz-okr/auth/feishu/device", 403},
		{"GET", "/api/okr/../chat/sessions", 403}, {"GET", "/api/okr/krs/%2e%2e", 403},
		{"GET", "/api/okr//board", 403}, {"GET", "http://example.com/api/okr/board", 403},
		{"PUT", "/api/text-files/okr_agent_principles", 403}, {"GET", "/api/text-files/chat_system_prompt", 403},
		{"CONNECT", "127.0.0.1:18800", 403}, {"CONNECT", "evil.com:443", 403},
	} {
		t.Run(tc.method+tc.target, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.target, nil)
			r.Header.Set("Cookie", "jarvis_session=secret")
			r.Header.Set("Authorization", "Bearer secret")
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	if calls != 1 {
		t.Fatalf("forbidden requests reached upstream: %d", calls)
	}
}

func TestAccessRejectsUnsafeModelTargets(t *testing.T) {
	for _, host := range []string{"localhost:443", "127.0.0.1:443", "*.openai.com:443", "api.openai.com:80", "api.openai.com", "api.openai.com:443/path"} {
		_, err := NewAccessHandler("http://localhost:18800", []string{host})
		if err == nil {
			t.Errorf("accepted %s", host)
		}
	}
	h, err := NewAccessHandler("http://127.0.0.1:18800", []string{"localhost:443"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("CONNECT", "localhost:443", nil))
	if w.Code != 403 || !strings.Contains(w.Body.String(), "address denied") {
		t.Fatalf("loopback resolution was not rejected: %d %s", w.Code, w.Body.String())
	}
}
