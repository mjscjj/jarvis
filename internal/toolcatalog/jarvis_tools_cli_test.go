package toolcatalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJarvisToolsHelpStatesDesignPrinciples(t *testing.T) {
	out, err := runJarvisTools(t, "", nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Simple first", "Progressive loading", "query-captured-resources", "create-project"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestJarvisToolsRejectsFlagsFromAnotherCommand(t *testing.T) {
	_, err := runJarvisTools(t, "http://unused.test", nil, "list-projects", "--id", "7")
	if err == nil || !strings.Contains(err.Error(), "list-projects does not accept --id") {
		t.Fatalf("error = %v", err)
	}
}

func TestJarvisToolsListCommandsReturnCompactSummaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/todos":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"page":1,"page_size":20,"items":[{"id":1,"title":"todo","description":"large","context_snapshot":{"large":true},"status":"extracted"}]}}`)
		case "/api/tasks":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"page":1,"page_size":20,"items":[{"id":2,"title":"task","background":"large","source_payload":{"large":true},"execution_result":"large","status":"done"}]}}`)
		case "/api/scheduled-tasks":
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"id":3,"title":"timer","instruction":"large","dispatch_payload":{"large":true},"context_snapshot":{"large":true},"status":"active"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	checks := []struct {
		command   string
		forbidden []string
	}{
		{"list-todos", []string{"description", "context_snapshot"}},
		{"list-tasks", []string{"background", "source_payload", "execution_result"}},
		{"list-scheduled-tasks", []string{"instruction", "dispatch_payload", "context_snapshot"}},
	}
	for _, check := range checks {
		t.Run(check.command, func(t *testing.T) {
			out, err := runJarvisTools(t, server.URL, nil, check.command)
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range check.forbidden {
				if strings.Contains(out, `"`+field+`"`) {
					t.Fatalf("%s leaked %s: %s", check.command, field, out)
				}
			}
		})
	}
}

func TestJarvisToolsGetTaskLoadsLargeRunFieldsOnlyWhenRequested(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tasks/9":
			fmt.Fprint(w, `{"code":0,"data":{"id":9,"title":"task"}}`)
		case "/api/tasks/9/runs":
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"id":1,"prompt":"secret prompt","output":"large output","status":"done"}]}}`)
		case "/api/tasks/9/events", "/api/facts":
			fmt.Fprint(w, `{"code":0,"data":{"items":[]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := runJarvisTools(t, server.URL, nil, "get-task", "--id", "9")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secret prompt") || strings.Contains(out, "large output") {
		t.Fatalf("default get-task leaked large fields: %s", out)
	}
	out, err = runJarvisTools(t, server.URL, nil, "get-task", "--id", "9", "--include-prompt", "--include-run-output")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "secret prompt") || !strings.Contains(out, "large output") {
		t.Fatalf("explicit get-task omitted requested fields: %s", out)
	}
}

func TestJarvisToolsGetScheduledTaskUsesExactEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scheduled-tasks/17" {
			t.Fatalf("path = %q, want exact scheduled-task endpoint", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"id":17,"instruction":"full"}}`)
	}))
	defer server.Close()
	out, err := runJarvisTools(t, server.URL, nil, "get-scheduled-task", "--id", "17")
	if err != nil || !strings.Contains(out, `"id":17`) {
		t.Fatalf("output = %s, error = %v", out, err)
	}
}

func TestJarvisToolsGetContextPassesChatAndProjectScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/context" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload struct {
			ChatID    string  `json:"chat_id"`
			ProjectID *uint64 `json:"project_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ChatID != "oc_runtime" || payload.ProjectID == nil || *payload.ProjectID != 45 {
			t.Fatalf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"snapshot_version":"v1","project":{"id":45}}}`)
	}))
	defer server.Close()
	out, err := runJarvisTools(t, server.URL, nil, "get-context", "--chat-id", "oc_runtime", "--project-id", "45")
	if err != nil || !strings.Contains(out, `"snapshot_version":"v1"`) {
		t.Fatalf("output = %s, error = %v", out, err)
	}
}

func TestJarvisToolsWorldModelWritesUseSpecificEndpoints(t *testing.T) {
	tests := []struct {
		command string
		args    []string
		method  string
		path    string
	}{
		{"create-project", []string{"--payload", `{"name":"p"}`}, http.MethodPost, "/api/projects"},
		{"update-project", []string{"--id", "7", "--payload", `{"name":"p"}`}, http.MethodPut, "/api/projects/7"},
		{"archive-project", []string{"--id", "7"}, http.MethodDelete, "/api/projects/7"},
		{"update-group", []string{"--id", "8", "--payload", `{}`}, http.MethodPut, "/api/groups/8"},
		{"update-principal", []string{"--payload", `{"name":"me"}`}, http.MethodPut, "/api/profile"},
		{"create-person", []string{"--payload", `{"name":"a"}`}, http.MethodPost, "/api/persons"},
		{"update-person", []string{"--id", "9", "--payload", `{"name":"a"}`}, http.MethodPut, "/api/persons/9"},
		{"delete-person", []string{"--id", "9"}, http.MethodDelete, "/api/persons/9"},
		{"create-resource", []string{"--payload", `{"title":"r"}`}, http.MethodPost, "/api/resources"},
		{"update-resource", []string{"--id", "10", "--payload", `{"title":"r"}`}, http.MethodPut, "/api/resources/10"},
		{"delete-resource", []string{"--id", "10"}, http.MethodDelete, "/api/resources/10"},
		{"create-relation", []string{"--payload", `{"description":"r"}`}, http.MethodPost, "/api/relation-facts"},
		{"update-relation", []string{"--id", "11", "--payload", `{"description":"r"}`}, http.MethodPut, "/api/relation-facts/11"},
		{"delete-relation", []string{"--id", "11"}, http.MethodDelete, "/api/relation-facts/11"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method || r.URL.Path != tt.path {
					t.Fatalf("request = %s %s, want %s %s", r.Method, r.URL.Path, tt.method, tt.path)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || !json.Valid(body) {
					t.Fatalf("body = %q, error = %v", body, err)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"code":0,"data":{"ok":true}}`)
			}))
			defer server.Close()
			if _, err := runJarvisTools(t, server.URL, nil, append([]string{tt.command}, tt.args...)...); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestJarvisToolsProvenanceDoesNotBorrowTaskIDForExplicitSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["source_kind"] != "manual_review" || payload["source_id"] != nil {
			t.Fatalf("provenance = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"id":1}}`)
	}))
	defer server.Close()
	_, err := runJarvisTools(t, server.URL, []string{"JARVIS_TASK_ID=42"},
		"append-fact", "--subject-type", "project", "--subject-id", "1",
		"--description", "decision", "--source", "manual_review")
	if err != nil {
		t.Fatal(err)
	}
}

func TestJarvisToolsTodoActorComesFromAgentStage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["actor"] != "extract" {
			t.Fatalf("actor = %#v, want extract", payload["actor"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"id":1}}`)
	}))
	defer server.Close()
	_, err := runJarvisTools(t, server.URL, []string{"JARVIS_AGENT_STAGE=extract"},
		"set-todo-status", "--id", "1", "--status", "observing", "--reason", "no action")
	if err != nil {
		t.Fatal(err)
	}
}

func TestJarvisToolsCreateTaskIsProactiveOnlyAndForcesStrongTaskContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/tasks" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["source_type"] != "proactive" {
			t.Fatalf("payload = %#v", payload)
		}
		if _, exists := payload["execution_mode"]; exists {
			t.Fatalf("payload still contains execution_mode: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"id":19,"source_type":"proactive","status":"pending"}}`)
	}))
	defer server.Close()
	payload := `{"title":"推进阻塞","action_type":"agent_task","target":"完成目标","background":{"why_now":"条件已满足"}}`
	out, err := runJarvisTools(t, server.URL, []string{"JARVIS_AGENT_STAGE=proactive"}, "create-task", "--payload", payload)
	if err != nil || !strings.Contains(out, `"id":19`) {
		t.Fatalf("output = %s, error = %v", out, err)
	}
	if _, err := runJarvisTools(t, server.URL, []string{"JARVIS_AGENT_STAGE=execute"}, "create-task", "--payload", payload); err == nil {
		t.Fatal("create-task succeeded outside proactive stage")
	}
}

func runJarvisTools(t *testing.T, apiBase string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-tools"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append(command.Environ(), extraEnv...)
	if apiBase != "" {
		command.Env = append(command.Env, "JARVIS_API_BASE="+apiBase)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("jarvis-tools %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return string(output), nil
}
