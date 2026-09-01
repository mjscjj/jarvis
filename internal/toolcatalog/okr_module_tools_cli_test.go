package toolcatalog

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type moduleToolRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

func runModuleTool(t *testing.T, script string, apiBase string, args ...string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "scripts", script))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "bash", append([]string{path}, args...)...)
	command.Env = append(command.Environ(), "JARVIS_API_BASE="+apiBase)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v: %s", script, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func TestOKRModuleToolsExposeReadsAndOnlyTagDefinitionWrites(t *testing.T) {
	requests := make(chan moduleToolRequest, 4)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests <- moduleToolRequest{Method: request.Method, Path: request.URL.Path, Query: request.URL.RawQuery, Body: string(body)}
		response.Header().Set("content-type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer server.Close()

	runModuleTool(t, "okr-module-tools", server.URL, "get-kr", "--id", "kr-1")
	request := <-requests
	if request.Method != http.MethodGet || request.Path != "/api/okr/krs/kr-1" {
		t.Fatalf("get-kr request = %+v", request)
	}
	runModuleTool(t, "okr-module-tools", server.URL, "people-search", "--query", "张 三")
	request = <-requests
	if request.Method != http.MethodGet || request.Path != "/api/okr/people/search" || !strings.Contains(request.Query, "q=") {
		t.Fatalf("people-search request = %+v", request)
	}
	runModuleTool(t, "okr-module-tools", server.URL, "board")
	request = <-requests
	if request.Method != http.MethodGet || request.Path != "/api/okr/board" {
		t.Fatalf("default board request = %+v", request)
	}

	help := runModuleTool(t, "okr-module-tools", server.URL, "--help")
	for _, forbidden := range []string{"create-objective", "create-kr", "replace-kr", "delete-kr"} {
		if strings.Contains(help, "\n  "+forbidden+" ") {
			t.Fatalf("OKR Agent help exposed %q: %s", forbidden, help)
		}
	}
}

func TestOKRTagToolsPreservePayloadAndSurfaceConflicts(t *testing.T) {
	const payload = `{"expected_version":7,"tags":[{"type":"custom","value":"双周报-官网SEO"},{"type":"region","value":"eu"}]}`
	requests := make(chan moduleToolRequest, 3)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		requests <- moduleToolRequest{Method: request.Method, Path: request.URL.Path, Body: string(body)}
		response.Header().Set("content-type", "application/json")
		response.WriteHeader(http.StatusConflict)
		_, _ = response.Write([]byte(`{"code":40923,"msg":"version conflict","data":{"version":8}}`))
	}))
	defer server.Close()
	path, err := filepath.Abs(filepath.Join("..", "..", "scripts", "okr-module-tools"))
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{payload, "-"} {
		command := exec.CommandContext(t.Context(), "bash", path, "--base-url", server.URL, "replace-kr-tags", "--id", "kr 标签", "--payload", input)
		command.Stdin = strings.NewReader(payload)
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), `"version":8`) {
			t.Fatalf("conflict must exit nonzero and preserve response: err=%v output=%s", err, output)
		}
		request := <-requests
		if request.Method != http.MethodPut || request.Path != "/api/okr/krs/kr 标签/tags" || request.Body != payload {
			t.Fatalf("tag request = %+v", request)
		}
	}
	command := exec.CommandContext(t.Context(), "bash", path, "--base-url", server.URL, "replace-point-tags", "--id", "策略 要点", "--payload", payload)
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), `"version":8`) {
		t.Fatalf("point conflict must exit nonzero and preserve response: err=%v output=%s", err, output)
	}
	request := <-requests
	if request.Method != http.MethodPut || request.Path != "/api/okr/points/策略 要点/tags" || request.Body != payload {
		t.Fatalf("point tag request = %+v", request)
	}
}

func TestWeeklyReportToolsExposeAtomicWrites(t *testing.T) {
	requests := make(chan moduleToolRequest, 16)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests <- moduleToolRequest{Method: request.Method, Path: request.URL.Path, Query: request.URL.RawQuery, Body: string(body)}
		response.Header().Set("content-type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer server.Close()

	tests := []struct {
		args   []string
		method string
		path   string
		query  string
	}{
		{[]string{"open-week", "--quarter", "2026-Q3", "--week", "2026-W36"}, http.MethodPost, "/api/weekly-report/weeks", ""},
		{[]string{"delete-week", "--quarter", "2026-Q3", "--week", "2026-W36"}, http.MethodDelete, "/api/weekly-report/weeks/2026-W36", "quarter=2026-Q3"},
		{[]string{"get-weekly-kr", "--id", "kr-1", "--week", "2026-W36"}, http.MethodGet, "/api/weekly-report/krs/kr-1", "week=2026-W36"},
		{[]string{"replace-weekly-core", "--id", "kr-1", "--payload", `{"expected_version":0,"week":"2026-W36","metric_note":"测试","metrics":[]}`}, http.MethodPut, "/api/weekly-report/krs/kr-1/core", ""},
		{[]string{"replace-score", "--target-kind", "kr", "--id", "kr-1", "--payload", `{"quarter":"2026-Q3","week":"2026-W36","score":0.7,"expected_version":0}`}, http.MethodPut, "/api/weekly-report/scores/kr/kr-1", ""},
		{[]string{"delete-score", "--target-kind", "point", "--id", "point-1", "--payload", `{"quarter":"2026-Q3","week":"2026-W36","expected_version":1}`}, http.MethodDelete, "/api/weekly-report/scores/point/point-1", ""},
		{[]string{"comments", "--quarter", "2026-Q3", "--week", "2026-W36"}, http.MethodGet, "/api/weekly-report/comments", ""},
		{[]string{"create-comment", "--payload", `{"quarter":"2026-Q3","week":"2026-W36","content":"建议"}`}, http.MethodPost, "/api/weekly-report/comments", ""},
		{[]string{"update-comment", "--id", "comment-1", "--payload", `{"todo":true}`}, http.MethodPut, "/api/weekly-report/comments/comment-1", ""},
		{[]string{"delete-comment", "--id", "comment-1"}, http.MethodDelete, "/api/weekly-report/comments/comment-1", ""},
		{[]string{"create-progress", "--point-id", "point-1", "--payload", `{"id":"agent-1","expected_version":0,"week":"2026-W36","status":"in_progress","text":"进展"}`}, http.MethodPost, "/api/weekly-report/points/point-1/progress", ""},
		{[]string{"update-progress", "--id", "agent-1", "--payload", `{"expected_version":2,"week":"2026-W36","status":"done","text":"完成"}`}, http.MethodPut, "/api/weekly-report/progress/agent-1", ""},
		{[]string{"delete-progress", "--id", "agent-1", "--payload", `{"expected_version":3}`}, http.MethodDelete, "/api/weekly-report/progress/agent-1", ""},
		{[]string{"meego-preview", "--quarter", "2026-Q3", "--week", "2026-W36"}, http.MethodGet, "/api/weekly-report/meego-preview", ""},
		{[]string{"point-meego-preview", "--point-id", "point-1", "--week", "2026-W36"}, http.MethodGet, "/api/weekly-report/points/point-1/meego-preview", ""},
		{[]string{"confirm-meego-progress", "--point-id", "point-1", "--payload", `{"expected_version":3,"week":"2026-W36","meego_work_item_id":"wi-1","status":"done","text":"完成"}`}, http.MethodPost, "/api/weekly-report/points/point-1/meego-confirm", ""},
	}
	for _, test := range tests {
		runModuleTool(t, "weekly-report-tools", server.URL, test.args...)
		request := <-requests
		if request.Method != test.method || request.Path != test.path {
			t.Fatalf("%v request = %+v, want %s %s", test.args, request, test.method, test.path)
		}
		if test.query != "" && !strings.Contains(request.Query, test.query) {
			t.Fatalf("%v query = %q, want to contain %q", test.args, request.Query, test.query)
		}
		if request.Method != http.MethodGet && request.Method != http.MethodDelete || strings.Contains(strings.Join(test.args, " "), "delete-progress") {
			if request.Body != "" && !json.Valid([]byte(request.Body)) {
				t.Fatalf("%v sent invalid JSON: %s", test.args, request.Body)
			}
		}
	}
}

func TestModuleToolsAllowDefaultScopedReads(t *testing.T) {
	requests := make(chan moduleToolRequest, 4)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- moduleToolRequest{Method: request.Method, Path: request.URL.Path, Query: request.URL.RawQuery}
		response.Header().Set("content-type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"data":{"objectives":[]}}`))
	}))
	defer server.Close()

	for _, call := range []struct {
		script string
		args   []string
		path   string
	}{
		{"okr-module-tools", []string{"board"}, "/api/okr/board"},
		{"weekly-report-tools", []string{"weeks"}, "/api/weekly-report/weeks"},
		{"weekly-report-tools", []string{"board"}, "/api/weekly-report/board"},
	} {
		runModuleTool(t, call.script, server.URL, call.args...)
		request := <-requests
		if request.Method != http.MethodGet || request.Path != call.path {
			t.Fatalf("%s %v request = %+v", call.script, call.args, request)
		}
	}
}
