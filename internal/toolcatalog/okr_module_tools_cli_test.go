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

func runModuleToolFailure(t *testing.T, script string, apiBase string, args ...string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "scripts", script))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "bash", append([]string{path}, args...)...)
	command.Env = append(command.Environ(), "JARVIS_API_BASE="+apiBase)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("%s %s unexpectedly succeeded: %s", script, strings.Join(args, " "), output)
	}
	return string(output)
}

func TestOKRModuleToolsExposeGenericReads(t *testing.T) {
	requests := make(chan moduleToolRequest, 6)
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
	runModuleTool(t, "biz-okr-tools", server.URL, "people-search", "--query", "张 三")
	request = <-requests
	if request.Method != http.MethodGet || request.Path != "/api/biz-okr/people/search" || !strings.Contains(request.Query, "q=") {
		t.Fatalf("people-search request = %+v", request)
	}
	runModuleTool(t, "okr-module-tools", server.URL, "board")
	request = <-requests
	if request.Method != http.MethodGet || request.Path != "/api/okr/board" {
		t.Fatalf("default board request = %+v", request)
	}
	runModuleTool(t, "okr-module-tools", server.URL, "list-objectives", "--quarter", "2026-Q3")
	request = <-requests
	if request.Method != http.MethodGet || request.Path != "/api/okr/objectives" || !strings.Contains(request.Query, "quarter=2026-Q3") {
		t.Fatalf("list-objectives request = %+v", request)
	}
	runModuleTool(t, "okr-module-tools", server.URL, "get-objective", "--id", "o one")
	request = <-requests
	if request.Method != http.MethodGet || request.Path != "/api/okr/objectives/o one" {
		t.Fatalf("get-objective request = %+v", request)
	}

	help := runModuleTool(t, "okr-module-tools", server.URL, "--help")
	for _, command := range []string{"replace-kr", "list-objectives", "get-objective", "projection-audit"} {
		if !strings.Contains(help, "\n  "+command) {
			t.Fatalf("OKR Agent help is missing %q: %s", command, help)
		}
	}
	for _, forbidden := range []string{"create-objective", "create-kr", "delete-kr"} {
		if strings.Contains(help, "\n  "+forbidden+" ") {
			t.Fatalf("OKR Agent help exposed %q: %s", forbidden, help)
		}
	}
}

func TestOKRProjectionAuditCombinesBothRelationDirections(t *testing.T) {
	requests := make(chan moduleToolRequest, 16)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- moduleToolRequest{Method: request.Method, Path: request.URL.Path, Query: request.URL.RawQuery}
		response.Header().Set("content-type", "application/json")
		switch request.URL.Path {
		case "/api/okr/objectives":
			_, _ = response.Write([]byte(`{"code":0,"data":{"quarter":"2026-Q3","objectives":[{"id":"o-1","title":"目标","kr_count":1,"metric_count":0,"point_count":1,"kr_owner_count":1,"point_owner_count":1}],"totals":{"objectives":1,"krs":1,"metrics":0,"points":1,"kr_owner_occurrences":1,"point_owner_occurrences":1}}}`))
		case "/api/okr/objectives/o-1":
			_, _ = response.Write([]byte(`{"code":0,"data":{"quarter":"2026-Q3","objective":{"id":"o-1","title":"目标","krs":[{"id":"kr-1","title":"结果","points":[{"id":"p-1","title":"拆解"}]}]}}}`))
		case "/api/relations":
			query := request.URL.Query()
			switch {
			case query.Get("source_type") == "okr_kr":
				_, _ = response.Write([]byte(`{"code":0,"data":{"items":[{"id":1,"source_type":"okr_kr","source_id":"kr-1","relation_type":"maps_to","target_type":"key_matter","target_id":"7","confirmed_at":"2026-09-07T00:00:00Z"},{"id":3,"source_type":"okr_kr","source_id":"kr-removed","relation_type":"maps_to","target_type":"key_matter","target_id":"8","confirmed_at":"2026-09-01T00:00:00Z"}]}}`))
			case query.Get("source_type") == "okr_point":
				_, _ = response.Write([]byte(`{"code":0,"data":{"items":[{"id":2,"source_type":"okr_point","source_id":"p-1","relation_type":"advances","target_type":"key_matter","target_id":"9","confirmed_at":"2026-09-07T00:00:00Z"}]}}`))
			case query.Get("target_type") == "okr_objective":
				_, _ = response.Write([]byte(`{"code":0,"data":{"items":[{"id":4,"source_type":"project","source_id":"10","relation_type":"mapped_from","target_type":"okr_objective","target_id":"o-1","confirmed_at":"2026-09-07T00:00:00Z"}]}}`))
			default:
				_, _ = response.Write([]byte(`{"code":0,"data":{"items":[]}}`))
			}
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	output := runModuleTool(t, "okr-module-tools", server.URL, "projection-audit", "--quarter", "2026-Q3")
	var payload struct {
		Data struct {
			Relations struct {
				Count          int            `json:"count"`
				ConfirmedCount int            `json:"confirmed_count"`
				ByWorldType    map[string]int `json:"by_world_type"`
			} `json:"relations"`
			Coverage map[string]struct {
				Total   int `json:"total"`
				Related int `json:"related"`
			} `json:"coverage"`
			Orphans []struct {
				ID int `json:"id"`
			} `json:"orphan_relations"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("audit output is not JSON: %v: %s", err, output)
	}
	if payload.Data.Relations.Count != 3 || payload.Data.Relations.ConfirmedCount != 3 || payload.Data.Relations.ByWorldType["project"] != 1 || payload.Data.Relations.ByWorldType["key_matter"] != 2 {
		t.Fatalf("relation audit = %+v", payload.Data.Relations)
	}
	if payload.Data.Coverage["objectives"].Related != 0 || payload.Data.Coverage["krs"].Related != 1 || payload.Data.Coverage["points"].Related != 1 || len(payload.Data.Orphans) != 1 || payload.Data.Orphans[0].ID != 3 {
		t.Fatalf("coverage = %+v orphans=%+v", payload.Data.Coverage, payload.Data.Orphans)
	}
	close(requests)
}

func TestOKRProjectionAuditReadsAllRelationPages(t *testing.T) {
	largeEvidence := strings.Repeat("完整业务 Ontology 关系证据", 128)
	relations := make([]map[string]any, 200)
	for index := range relations {
		relations[index] = map[string]any{
			"id":            index + 1,
			"source_type":   "okr_objective",
			"source_id":     "o-1",
			"relation_type": "maps_to",
			"target_type":   "project",
			"target_id":     index + 1,
			"evidence":      map[string]any{"basis": largeEvidence},
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("content-type", "application/json")
		switch request.URL.Path {
		case "/api/okr/objectives":
			_, _ = response.Write([]byte(`{"code":0,"data":{"quarter":"2026-Q3","objectives":[{"id":"o-1"}],"totals":{"objectives":1,"krs":0,"metrics":0,"points":0,"kr_owner_occurrences":0,"point_owner_occurrences":0}}}`))
		case "/api/okr/objectives/o-1":
			_, _ = response.Write([]byte(`{"code":0,"data":{"quarter":"2026-Q3","objective":{"id":"o-1","title":"目标","krs":[]}}}`))
		case "/api/relations":
			if request.URL.Query().Get("source_type") == "okr_objective" {
				if request.URL.Query().Get("cursor") == "2" {
					_ = json.NewEncoder(response).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []map[string]any{{
						"id": 201, "source_type": "okr_objective", "source_id": "o-1",
						"relation_type": "maps_to", "target_type": "project", "target_id": 201,
					}}}})
					return
				}
				_ = json.NewEncoder(response).Encode(map[string]any{"code": 0, "data": map[string]any{"items": relations, "next_cursor": "2"}})
				return
			}
			_, _ = response.Write([]byte(`{"code":0,"data":{"items":[]}}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	output := runModuleTool(t, "okr-module-tools", server.URL, "projection-audit", "--quarter", "2026-Q3")
	var payload struct {
		Data struct {
			Relations struct {
				Count int `json:"count"`
			} `json:"relations"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("audit output is not JSON: %v: %s", err, output)
	}
	if payload.Data.Relations.Count != 201 {
		t.Fatalf("relation count = %d, want 201", payload.Data.Relations.Count)
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
	path, err := filepath.Abs(filepath.Join("..", "..", "scripts", "biz-okr-tools"))
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
		if request.Method != http.MethodPut || request.Path != "/api/biz-okr/krs/kr 标签/tags" || request.Body != payload {
			t.Fatalf("tag request = %+v", request)
		}
	}
	command := exec.CommandContext(t.Context(), "bash", path, "--base-url", server.URL, "replace-point-tags", "--point-id", "策略 要点", "--payload", payload)
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), `"version":8`) {
		t.Fatalf("point conflict must exit nonzero and preserve response: err=%v output=%s", err, output)
	}
	request := <-requests
	if request.Method != http.MethodPut || request.Path != "/api/biz-okr/points/策略 要点/tags" || request.Body != payload {
		t.Fatalf("point tag request = %+v", request)
	}
}

func TestOKRAndBizToolsExposeAtomicWrites(t *testing.T) {
	requests := make(chan moduleToolRequest, 16)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests <- moduleToolRequest{Method: request.Method, Path: request.URL.Path, Query: request.URL.RawQuery, Body: string(body)}
		response.Header().Set("content-type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer server.Close()

	runModuleTool(t, "okr-module-tools", server.URL, "get-weekly-kr", "--id", "kr-1", "--week", "2026-W36")
	request := <-requests
	if request.Method != http.MethodGet || request.Path != "/api/okr/krs/kr-1/weekly" || !strings.Contains(request.Query, "week=2026-W36") {
		t.Fatalf("generic get-weekly-kr request = %+v", request)
	}
	runModuleTool(t, "okr-module-tools", server.URL, "replace-kr", "--id", "kr-1", "--payload", `{"expected_version":0,"title":"目标","metric_note":"口径","metrics":[],"points":[],"owners":[]}`)
	request = <-requests
	if request.Method != http.MethodPut || request.Path != "/api/okr/krs/kr-1" || !json.Valid([]byte(request.Body)) {
		t.Fatalf("generic replace-kr request = %+v", request)
	}

	tests := []struct {
		args   []string
		method string
		path   string
		query  string
	}{
		{[]string{"open-week", "--quarter", "2026-Q3", "--week", "2026-W36"}, http.MethodPost, "/api/okr/weeks", ""},
		{[]string{"delete-week", "--quarter", "2026-Q3", "--week", "2026-W36"}, http.MethodDelete, "/api/biz-okr/weeks/2026-W36", "quarter=2026-Q3"},
		{[]string{"get-weekly-kr", "--id", "kr-1", "--week", "2026-W36"}, http.MethodGet, "/api/biz-okr/krs/kr-1/weekly", "week=2026-W36"},
		{[]string{"replace-weekly-core", "--id", "kr-1", "--payload", `{"expected_version":0,"week":"2026-W36","metric_note":"测试","metrics":[]}`}, http.MethodPut, "/api/okr/krs/kr-1/weekly-core", ""},
		{[]string{"replace-score", "--target-kind", "kr", "--id", "kr-1", "--payload", `{"quarter":"2026-Q3","week":"2026-W36","score":0.7,"expected_version":0}`}, http.MethodPut, "/api/biz-okr/scores/kr/kr-1", ""},
		{[]string{"delete-score", "--target-kind", "point", "--id", "point-1", "--payload", `{"quarter":"2026-Q3","week":"2026-W36","expected_version":1}`}, http.MethodDelete, "/api/biz-okr/scores/point/point-1", ""},
		{[]string{"comments", "--quarter", "2026-Q3", "--week", "2026-W36"}, http.MethodGet, "/api/biz-okr/comments", ""},
		{[]string{"create-comment", "--payload", `{"quarter":"2026-Q3","week":"2026-W36","content":"建议"}`}, http.MethodPost, "/api/biz-okr/comments", ""},
		{[]string{"update-comment", "--id", "comment-1", "--payload", `{"todo":true}`}, http.MethodPut, "/api/biz-okr/comments/comment-1", ""},
		{[]string{"delete-comment", "--id", "comment-1"}, http.MethodDelete, "/api/biz-okr/comments/comment-1", ""},
		{[]string{"follow-ups", "--quarter", "2026-Q3", "--week", "2026-W36"}, http.MethodGet, "/api/biz-okr/follow-ups", "quarter=2026-Q3"},
		{[]string{"get-follow-up", "--id", "follow-up-1"}, http.MethodGet, "/api/biz-okr/follow-ups/follow-up-1", ""},
		{[]string{"create-follow-up", "--payload", `{"id":"follow-up-1","expected_version":0,"quarter":"2026-Q3","week":"2026-W36","topic":"事项","owners":[{"open_id":"ou_1","name":"负责人"}],"status":"not_started"}`}, http.MethodPost, "/api/biz-okr/follow-ups", ""},
		{[]string{"update-follow-up", "--id", "follow-up-1", "--payload", `{"expected_version":1,"quarter":"2026-Q3","week":"2026-W36","topic":"事项","owners":[{"open_id":"ou_1","name":"负责人"}],"status":"abandoned"}`}, http.MethodPut, "/api/biz-okr/follow-ups/follow-up-1", ""},
		{[]string{"delete-follow-up", "--id", "follow-up-1", "--payload", `{"expected_version":2}`}, http.MethodDelete, "/api/biz-okr/follow-ups/follow-up-1", ""},
		{[]string{"create-progress", "--point-id", "point-1", "--payload", `{"id":"agent-1","expected_version":0,"week":"2026-W36","status":"in_progress","text":"进展"}`}, http.MethodPost, "/api/okr/points/point-1/progress", ""},
		{[]string{"update-progress", "--id", "agent-1", "--payload", `{"expected_version":2,"week":"2026-W36","status":"done","text":"完成"}`}, http.MethodPut, "/api/okr/progress/agent-1", ""},
		{[]string{"delete-progress", "--id", "agent-1", "--payload", `{"expected_version":3}`}, http.MethodDelete, "/api/okr/progress/agent-1", ""},
		{[]string{"meego-preview", "--quarter", "2026-Q3", "--week", "2026-W36"}, http.MethodGet, "/api/biz-okr/meego-preview", ""},
		{[]string{"point-meego-preview", "--point-id", "point-1", "--week", "2026-W36"}, http.MethodGet, "/api/biz-okr/points/point-1/meego-preview", ""},
		{[]string{"confirm-meego-progress", "--point-id", "point-1", "--payload", `{"expected_version":3,"week":"2026-W36","meego_work_item_id":"wi-1","status":"done","text":"完成"}`}, http.MethodPost, "/api/biz-okr/points/point-1/meego-confirm", ""},
	}
	for _, test := range tests {
		script := "biz-okr-tools"
		switch test.args[0] {
		case "open-week", "replace-weekly-core", "create-progress", "update-progress", "delete-progress":
			script = "okr-module-tools"
		}
		runModuleTool(t, script, server.URL, test.args...)
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

// Whole-week deletion is Biz-owned because comments, scores, follow-ups and
// reminder batches are keyed by (quarter, week) with no foreign key to the week
// anchor. A generic half-delete would resurface them on the next week opened
// under the same key, so the generic tool must not offer the command at all.
func TestGenericOKRToolsRejectWeekDeletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		t.Error("okr-module-tools delete-week reached the server")
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	path, err := filepath.Abs(filepath.Join("..", "..", "scripts", "okr-module-tools"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "bash", path, "delete-week", "--quarter", "2026-Q3", "--week", "2026-W36")
	command.Env = append(command.Environ(), "JARVIS_API_BASE="+server.URL)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("okr-module-tools accepted delete-week: %s", output)
	}
	if !strings.Contains(string(output), "unknown command") {
		t.Fatalf("delete-week rejection = %s", output)
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
		{"okr-module-tools", []string{"weeks"}, "/api/okr/weeks"},
		{"biz-okr-tools", []string{"board"}, "/api/biz-okr/board"},
	} {
		runModuleTool(t, call.script, server.URL, call.args...)
		request := <-requests
		if request.Method != http.MethodGet || request.Path != call.path {
			t.Fatalf("%s %v request = %+v", call.script, call.args, request)
		}
	}
}
