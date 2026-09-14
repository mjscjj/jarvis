package toolcatalog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorldOverviewCLIUsesSharedDefaultsAndPaging(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/world-overview" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.URL.Query().Get("section") == "" && r.URL.Query().Get("limit") != "0" {
			t.Errorf("CLI overrides shared defaults: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"sections":[],"read_at":"now"}}`))
	}))
	defer server.Close()
	for _, args := range [][]string{{"get-world-overview"}, {"get-world-overview", "--section", "task", "--offset", "10", "--limit", "3", "--id", "5"}} {
		out, err := runJarvisTools(t, server.URL, nil, args...)
		if err != nil || !strings.Contains(string(out), `"read_at":"now"`) {
			t.Fatalf("CLI %s %v", out, err)
		}
	}
}
