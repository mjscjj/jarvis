package toolcatalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestJarvisToolsAllHelpIncludesWorldCommands(t *testing.T) {
	out, err := runJarvisTools(t, "", nil, "help", "all")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"query-captured-resources", "create-project", "list-key-matters", "touch-key-matter", "touch-resource", "get-page", "resolve-world-node", "update-page", "list-pages", "list-page-revisions", "list-backlinks", "list-relations", "create-relation", "purge-world-entities", "get-world-progress", "create-world-progress", "update-world-progress"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestJarvisToolsResolvesRepositoryThroughSymlinkOutsideWorkingDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/profile" {
			t.Fatalf("request path = %s, want /api/profile", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"open_id":"ou_principal"}}`)
	}))
	defer server.Close()

	repoRoot := t.TempDir()
	scriptsDir := filepath.Join(repoRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceScript, err := os.ReadFile(filepath.Join("..", "..", "scripts", "jarvis-tools"))
	if err != nil {
		t.Fatal(err)
	}
	targetScript := filepath.Join(scriptsDir, "jarvis-tools")
	if err := os.WriteFile(targetScript, sourceScript, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"json-api-data.mjs", "lib"} {
		base := filepath.Join("..", "..", "scripts", source)
		if err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(filepath.Join("..", "..", "scripts"), path)
			if err != nil {
				return err
			}
			target := filepath.Join(scriptsDir, rel)
			if entry.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, content, 0o644)
		}); err != nil {
			t.Fatal(err)
		}
	}

	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "jarvis-tools")
	if err := os.Symlink(targetScript, link); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", link, "get-principal", "--api-base", server.URL)
	command.Dir = t.TempDir()
	command.Env = append(command.Environ(), "JARVIS_API_BASE="+server.URL)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("jarvis-tools through symlink: %v: %s", err, output)
	}
	if !strings.Contains(string(output), `"open_id":"ou_principal"`) {
		t.Fatalf("output = %s", output)
	}
}

func TestJarvisToolsCloseTaskHelpContainsOnlyMachineContract(t *testing.T) {
	out, err := runJarvisTools(t, "", nil, "close-task", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"trusted Jarvis Agent", "expected_version", "result.summary", "JARVIS_AGENT_STAGE", "--actor"} {
		if !strings.Contains(out, required) {
			t.Fatalf("close-task help missing machine contract %q:\n%s", required, out)
		}
	}
	for _, forbidden := range []string{"verified completion", "objective invalidation", "Crossing a day", "silence", "not a close reason"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("close-task help contains semantic close policy %q:\n%s", forbidden, out)
		}
	}
	if strings.Contains(out, "only available when JARVIS_AGENT_STAGE=proactive") {
		t.Fatalf("close-task help still advertises an internal stage gate:\n%s", out)
	}
}

func TestJarvisToolsMessageIDLookupIsExactAndCompact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/messages" || r.URL.Query().Get("message_ids") != "clue:source:123,om_other" || r.URL.Query().Get("limit") != "100" {
			t.Errorf("unexpected query: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"items":[{"id":7,"message_id":"clue:source:123","content":"full evidence"}]}}`)
	}))
	defer server.Close()
	out, err := runJarvisTools(t, server.URL, nil, "query-messages", "--message-ids", "clue:source:123,om_other", "--limit", "100")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"message_id":"clue:source:123"`) || strings.Contains(out, "full evidence") {
		t.Fatalf("lookup output = %s", out)
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
		case "/api/persons":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"page":1,"page_size":20,"items":[{"id":5,"open_id":"ou_alice","name":"Alice","en_name":"Alice","department":"Engineering","title":"Staff Engineer","role":"key","summary":"large","priority_weight":0.9,"is_active":true,"updated_at":"2026-08-07T10:00:00Z"}]}}`)
		case "/api/key-matters":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"page":1,"page_size":20,"items":[{"id":4,"title":"matter","status":"跟进中","summary":"current","project_id":1,"due_at":null,"last_progress_at":null,"last_active_at":"2026-08-07T10:00:00Z","closed_at":null,"project":{"large":true}}]}}`)
		case "/api/groups":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"page":1,"page_size":20,"broadened":false,"items":[{"id":8,"chat_id":"oc_group","name":"Project Group","description":"working group","project_id":3,"project":{"id":3,"code":"p3","name":"Project"},"tier":"warm","pinned":true,"include_in_memory":true,"is_key_group":false,"related_group":true,"last_active_at":1,"message_count":9}]}}`)
		case "/api/todos":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"page":1,"page_size":20,"items":[{"id":1,"title":"todo","description":"large","content":{"large":true},"status":"extracted"}]}}`)
		case "/api/tasks":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"page":1,"page_size":20,"items":[{"id":2,"title":"task","source_payload":{"large":true},"execution_result":"large","status":"done"}]}}`)
		case "/api/scheduled-tasks":
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"id":3,"title":"timer","schedule_type":"weekly","weekday":7,"daily_time":"09:00","instruction":"large","dispatch_payload":{"large":true},"context_snapshot":{"large":true},"status":"active"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	checks := []struct {
		command   string
		forbidden []string
	}{
		{"list-persons", []string{"summary", "priority_weight", "relation"}},
		{"list-key-matters", []string{"closed_at", "project"}},
		{"list-todos", []string{"description", "content"}},
		{"list-tasks", []string{"source_payload", "execution_result"}},
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
			if check.command == "list-scheduled-tasks" && (!strings.Contains(out, `"weekday":7`) || !strings.Contains(out, `"schedule_type":"weekly"`) || !strings.Contains(out, `"daily_time":"09:00"`)) {
				t.Fatalf("weekly schedule missing from summary: %s", out)
			}
		})
	}
	groupOut, err := runJarvisTools(t, server.URL, nil, "list-groups")
	if err != nil || !strings.Contains(groupOut, `"project_id":3`) || !strings.Contains(groupOut, `"include_in_memory":true`) {
		t.Fatalf("list-groups omitted control fields: output=%s error=%v", groupOut, err)
	}
}

func TestJarvisToolsGetTaskLoadsLargeRunFieldsOnlyWhenRequested(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tasks/9":
			fmt.Fprint(w, `{"code":0,"data":{"id":9,"title":"task"}}`)
		case "/api/task-runs/1":
			fmt.Fprint(w, `{"code":0,"data":{"id":1,"prompt":"secret prompt","output":"large output","status":"done"}}`)
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
	out, err = runJarvisTools(t, server.URL, nil, "get-task-run", "--id", "1", "--include-prompt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "secret prompt") || !strings.Contains(out, "large output") {
		t.Fatalf("explicit get-task omitted requested fields: %s", out)
	}
}

func TestJarvisToolsReadsAppendsAndSetsSharedMemory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/shared-memory" && r.URL.Path != "/api/shared-memory/append" {
			http.NotFound(w, r)
			return
		}
		switch {
		case r.Method == http.MethodGet:
			fmt.Fprint(w, `{"code":0,"data":{"content":"已有偏好"}}`)
		case r.Method == http.MethodPut:
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Content != "整理后的偏好" {
				t.Fatalf("set content = %q", body.Content)
			}
			fmt.Fprint(w, `{"code":0,"data":{"content":"整理后的偏好"}}`)
		case r.Method == http.MethodPost:
			var body struct {
				Note string `json:"note"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Note != "新增偏好" {
				t.Fatalf("append note = %q", body.Note)
			}
			fmt.Fprint(w, `{"code":0,"data":{"content":"已有偏好\n新增偏好"}}`)
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"get-shared-memory"}, want: "已有偏好"},
		{args: []string{"append-shared-memory", "--note", "新增偏好"}, want: "新增偏好"},
		{args: []string{"set-shared-memory", "--content", "整理后的偏好"}, want: "整理后的偏好"},
	} {
		out, err := runJarvisTools(t, server.URL, nil, test.args...)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, test.want) {
			t.Fatalf("%v output = %s", test.args, out)
		}
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

func TestJarvisToolsContextReadsPreserveNumericEvidence(t *testing.T) {
	// Envelope key order and escaped text must not affect exact data reads.
	const data = `{"id":9,"context":{"body":{"id":9007199254740993,"decimal":0.123456789012345678901,"text":"a } , \\\"data\\\": b","new_field":[true,null,{"value":1e400}]}},"prompt":""}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"meta":{"text":"data"},"data":`+data+`,"code":0}`)
	}))
	defer server.Close()
	for _, args := range [][]string{
		{"get-task", "--id", "9"},
		{"get-task", "--id", "9", "--context", "source"},
		{"get-todo", "--id", "9", "--context", "full"},
		{"get-task-run", "--id", "9", "--include-prompt"},
	} {
		out, err := runJarvisTools(t, server.URL, nil, args...)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out) != data {
			t.Fatalf("%v changed evidence:\n%s\nwant:\n%s", args, out, data)
		}
	}
}

func TestJarvisToolsGetContextPassesChatAndProjectScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/agent-identity" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"code":0,"data":{"display_name":"Silver"}}`)
			return
		}
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
	if err != nil || !strings.Contains(out, `"snapshot_version":"v1"`) ||
		!strings.Contains(out, `"agent_identity":{"display_name":"Silver"}`) {
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
		{"create-key-matter", []string{"--payload", `{"title":"m"}`}, http.MethodPost, "/api/key-matters"},
		{"update-key-matter", []string{"--id", "12", "--payload", `{"title":"m"}`}, http.MethodPut, "/api/key-matters/12"},
		{"touch-key-matter", []string{"--id", "12"}, http.MethodPost, "/api/key-matters/12/touch"},
		{"close-key-matter", []string{"--id", "12"}, http.MethodDelete, "/api/key-matters/12"},
		{"update-group", []string{"--id", "8", "--payload", `{"project_id":null,"related_group":false,"pinned":false,"include_in_memory":false,"is_key_group":false}`}, http.MethodPut, "/api/groups/8"},
		{"update-principal", []string{"--payload", `{"name":"me"}`}, http.MethodPut, "/api/profile"},
		{"create-person", []string{"--payload", `{"name":"a"}`}, http.MethodPost, "/api/persons"},
		{"update-person", []string{"--id", "9", "--payload", `{"name":"a"}`}, http.MethodPut, "/api/persons/9"},
		{"delete-person", []string{"--id", "9"}, http.MethodDelete, "/api/persons/9"},
		{"create-resource", []string{"--payload", `{"title":"r"}`}, http.MethodPost, "/api/resources"},
		{"update-resource", []string{"--id", "10", "--payload", `{"title":"r"}`}, http.MethodPut, "/api/resources/10"},
		{"touch-resource", []string{"--id", "10"}, http.MethodPost, "/api/resources/10/touch"},
		{"delete-resource", []string{"--id", "10"}, http.MethodDelete, "/api/resources/10"},
		{"update-page", []string{"--type", "project", "--id", "7", "--content", "hello", "--if-unchanged-since", "2026-08-15T00:00:00Z"}, http.MethodPut, "/api/pages/project/7"},
		{"create-relation", []string{"--payload", `{"source_type":"okr_kr","source_id":"kr-1","relation_type":"projects_to","target_type":"project","target_id":"7"}`}, http.MethodPost, "/api/relations"},
		{"purge-world-entities", []string{"--payload", `{"entities":[{"type":"project","id":8,"expected_name":"copy"}]}`}, http.MethodPost, "/api/world/purge"},
		{"get-page-guidance", nil, http.MethodGet, "/api/text-files/entity_page_guidance"},
		{"append-facts-batch", []string{"--payload", `[{"subject_type":"project","subject_id":1,"description":"d1","source":"system"},{"subject_type":"project","subject_id":2,"description":"d2","source":"system"}]`}, http.MethodPost, "/api/facts/batch"},
		{"create-world-progress", []string{"--payload", `{"expected_version":0,"subject_type":"okr_point","subject_id":"point-1","period_key":"2026-W36","signal":"yellow","summary":"waiting","evidence":{},"evidence_until":"2026-09-06T09:00:00Z"}`}, http.MethodPost, "/api/world-progress"},
		{"update-world-progress", []string{"--id", "11", "--payload", `{"expected_version":0,"signal":"green","summary":"done","evidence":{},"evidence_until":"2026-09-06T10:00:00Z"}`}, http.MethodPut, "/api/world-progress/11"},
		{"get-message", []string{"--id", "11"}, http.MethodGet, "/api/messages/11"},
		{"get-todo-event", []string{"--id", "12"}, http.MethodGet, "/api/todo-events/12"},
		{"get-task-event", []string{"--id", "13"}, http.MethodGet, "/api/task-events/13"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method || r.URL.Path != tt.path {
					t.Fatalf("request = %s %s, want %s %s", r.Method, r.URL.Path, tt.method, tt.path)
				}
				if tt.method != http.MethodGet {
					body, err := io.ReadAll(r.Body)
					if err != nil || !json.Valid(body) {
						t.Fatalf("body = %q, error = %v", body, err)
					}
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

func TestJarvisToolsWorldProgressUsesExactReadKeysAndAcceptsCreated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/world-progress/9":
			fmt.Fprint(w, `{"code":0,"data":{"id":9,"evidence":{"value":9007199254740993}}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/world-progress":
			if r.URL.Query().Get("subject_type") != "okr_point" || r.URL.Query().Get("subject_id") != "point / 1" || r.URL.Query().Get("period_key") != "2026-W36" {
				t.Fatalf("query = %v", r.URL.Query())
			}
			fmt.Fprint(w, `{"code":0,"data":{"id":10}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/world-progress":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"code":0,"data":{"id":11}}`)
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	out, err := runJarvisTools(t, server.URL, nil, "get-world-progress", "--id", "9")
	if err != nil || !strings.Contains(out, "9007199254740993") {
		t.Fatalf("id read output=%s error=%v", out, err)
	}
	out, err = runJarvisTools(t, server.URL, nil, "get-world-progress", "--subject-type", "okr_point", "--subject-id", "point / 1", "--period-key", "2026-W36")
	if err != nil || !strings.Contains(out, `"id":10`) {
		t.Fatalf("key read output=%s error=%v", out, err)
	}
	out, err = runJarvisTools(t, server.URL, nil, "create-world-progress", "--payload", `{"expected_version":0}`)
	if err != nil || !strings.Contains(out, `"id":11`) {
		t.Fatalf("create output=%s error=%v", out, err)
	}
}

func TestJarvisToolsAppendFactsBatchNormalizesMachinePayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/facts/batch" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) != 2 {
			t.Fatalf("payload = %#v", payload)
		}
		first := payload[0]
		if first["source_kind"] != "message" || first["source_id"] != float64(5) {
			t.Fatalf("first source pointer = %#v", first)
		}
		if _, exists := first["source"]; exists {
			t.Fatalf("payload leaked CLI source field: %#v", first)
		}
		occurredAt, ok := first["occurred_at"].(string)
		if !ok {
			t.Fatalf("default occurred_at = %#v", first["occurred_at"])
		}
		if _, err := time.Parse(time.RFC3339, occurredAt); err != nil {
			t.Fatalf("default occurred_at = %q: %v", occurredAt, err)
		}
		second := payload[1]
		if second["source_kind"] != "task_event" || second["source_id"] != float64(7) ||
			second["occurred_at"] != "2026-08-14T12:34:56+08:00" {
			t.Fatalf("explicit provenance = %#v", second)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"items":[]}}`)
	}))
	defer server.Close()
	payload := `[
		{"subject_type":"project","subject_id":1,"description":"事实 A","source":"message","source_id":5},
		{"subject_type":"task","subject_id":2,"description":"事实 B","occurred_at":"2026-08-14T12:34:56+08:00","source":"task_event","source_id":7}
	]`
	if _, err := runJarvisTools(t, server.URL, []string{"JARVIS_AGENT_STAGE=factengine"},
		"append-facts-batch", "--payload", payload); err != nil {
		t.Fatal(err)
	}
}

func TestJarvisToolsAppendFactsBatchRejectsInvalidFieldsBeforeRequest(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "unknown field", payload: `[{"subject_type":"project","subject_id":1,"description":"事实","fact_type":"decision"}]`},
		{name: "string source id", payload: `[{"subject_type":"project","subject_id":1,"description":"事实","source":"message","source_id":"1"}]`},
		{name: "source id without source", payload: `[{"subject_type":"project","subject_id":1,"description":"事实","source_id":1}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runJarvisTools(t, "http://127.0.0.1:1", []string{"JARVIS_AGENT_STAGE=factengine"},
				"append-facts-batch", "--payload", tt.payload)
			if err == nil || !strings.Contains(err.Error(), "payload is invalid") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestJarvisToolsGetKeyMatterUsesExactEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/key-matters/17" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"id":17,"title":"关键事项"}}`)
	}))
	defer server.Close()
	out, err := runJarvisTools(t, server.URL, nil, "get-key-matter", "--id", "17")
	if err != nil || !strings.Contains(out, `"id":17`) {
		t.Fatalf("output = %s, error = %v", out, err)
	}
}

func TestJarvisToolsPageCommandsUseExactEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/pages/project/7":
			fmt.Fprint(w, `{"code":0,"data":{"type":"project","id":7,"summary":"page","updated_at":"2026-08-15T00:00:00Z","fact_count":12}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/text-files/entity_page_guidance":
			fmt.Fprint(w, `{"code":0,"data":{"key":"entity_page_guidance","content":"shared page guidance"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/pages":
			if r.URL.Query().Get("type") != "person" || r.URL.Query().Get("all") != "true" ||
				r.URL.Query().Get("stale_days") != "14" || r.URL.Query().Get("over_limit") != "true" {
				t.Fatalf("list-pages query = %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"type":"person","id":12}]}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/pages/person/12/backlinks":
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"type":"project","id":7}]}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/world-nodes/okr_kr/kr-7":
			fmt.Fprint(w, `{"code":0,"data":{"type":"okr_kr","id":"kr-7","name":"增长 KR"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := runJarvisTools(t, server.URL, nil, "get-page", "--type", "project", "--id", "7")
	if err != nil || !strings.Contains(out, `"fact_count":12`) {
		t.Fatalf("get-page output = %s, error = %v", out, err)
	}
	out, err = runJarvisTools(t, server.URL, nil, "get-page-guidance")
	if err != nil || !strings.Contains(out, `"content":"shared page guidance"`) {
		t.Fatalf("get-page-guidance output = %s, error = %v", out, err)
	}
	out, err = runJarvisTools(t, server.URL, nil, "list-pages", "--type", "person", "--all", "--stale-days", "14", "--over-limit")
	if err != nil || !strings.Contains(out, `"id":12`) {
		t.Fatalf("list-pages output = %s, error = %v", out, err)
	}
	out, err = runJarvisTools(t, server.URL, nil, "list-backlinks", "--type", "person", "--id", "12")
	if err != nil || !strings.Contains(out, `"id":7`) {
		t.Fatalf("list-backlinks output = %s, error = %v", out, err)
	}
	out, err = runJarvisTools(t, server.URL, nil, "resolve-world-node", "--type", "okr_kr", "--id", "kr-7")
	if err != nil || !strings.Contains(out, `"name":"增长 KR"`) {
		t.Fatalf("resolve-world-node output = %s, error = %v", out, err)
	}
}

func TestJarvisToolsListPageRevisionsReadsAllCursorPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/pages/project/7/revisions" || r.URL.Query().Get("limit") != "1" {
			t.Fatalf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "7" {
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"id":6,"old_text":"older"}]}}`)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"items":[{"id":7,"old_text":"newer"}],"next_cursor":"7"}}`)
	}))
	defer server.Close()

	out, err := runJarvisTools(t, server.URL, nil, "list-page-revisions", "--type", "project", "--id", "7", "--limit", "1")
	if err != nil {
		t.Fatal(err)
	}
	var revisions []map[string]any
	if err := json.Unmarshal([]byte(out), &revisions); err != nil || len(revisions) != 2 {
		t.Fatalf("revisions = %#v, error = %v, output = %s", revisions, err, out)
	}
}

func TestJarvisToolsPageListsDoNotPassAccumulatedJSONAsArguments(t *testing.T) {
	largeText := strings.Repeat("完整世界模型正文", 1024)
	pages := make([]map[string]any, 200)
	revisions := make([]map[string]any, 100)
	for index := range pages {
		pages[index] = map[string]any{"type": "group", "id": index + 2, "name": fmt.Sprintf("group-%d", index), "index_line": largeText}
	}
	for index := range revisions {
		revisions[index] = map[string]any{"id": index + 2, "old_text": largeText}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/pages":
			if r.URL.Query().Get("cursor") == "group:2" {
				fmt.Fprint(w, `{"code":0,"data":{"items":[{"type":"group","id":1,"name":"last"}]}}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": pages, "next_cursor": "group:2"}})
		case "/api/pages/project/7/revisions":
			if r.URL.Query().Get("cursor") == "2" {
				fmt.Fprint(w, `{"code":0,"data":{"items":[{"id":1,"old_text":"last"}]}}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": revisions, "next_cursor": "2"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	output, err := runJarvisTools(t, server.URL, nil, "list-pages", "--all", "--limit", "200")
	if err != nil {
		t.Fatal(err)
	}
	var pageItems []map[string]any
	if err := json.Unmarshal([]byte(output), &pageItems); err != nil || len(pageItems) != 201 {
		t.Fatalf("page count = %d, error = %v", len(pageItems), err)
	}

	output, err = runJarvisTools(t, server.URL, nil, "list-page-revisions", "--type", "project", "--id", "7", "--limit", "100")
	if err != nil {
		t.Fatal(err)
	}
	var revisionItems []map[string]any
	if err := json.Unmarshal([]byte(output), &revisionItems); err != nil || len(revisionItems) != 101 {
		t.Fatalf("revision count = %d, error = %v", len(revisionItems), err)
	}
}

func TestJarvisToolsUpdatePageSurfacesConflictBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/pages/project/7" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload struct {
			Content          string `json:"content"`
			IfUnchangedSince string `json:"if_unchanged_since"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Content != "new page" || payload.IfUnchangedSince != "2026-08-15T00:00:00Z" {
			t.Fatalf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"code":40900,"data":{"summary":"current full page","updated_at":"2026-08-15T01:00:00Z"}}`)
	}))
	defer server.Close()
	_, err := runJarvisTools(t, server.URL, nil, "update-page", "--type", "project", "--id", "7",
		"--content", "new page", "--if-unchanged-since", "2026-08-15T00:00:00Z")
	if err == nil || !strings.Contains(err.Error(), "HTTP 409") || !strings.Contains(err.Error(), "current full page") {
		t.Fatalf("error = %v", err)
	}
}

func TestJarvisToolsUpdateCommandsRejectSummary(t *testing.T) {
	commands := [][]string{
		{"update-project", "--id", "7", "--payload", `{"summary":"no"}`},
		{"update-person", "--id", "9", "--payload", `{"summary":"no"}`},
		{"update-key-matter", "--id", "12", "--payload", `{"summary":"no"}`},
		{"update-resource", "--id", "10", "--payload", `{"summary":"no"}`},
		{"update-group", "--id", "8", "--payload", `{"summary":"no"}`},
		{"update-principal", "--payload", `{"summary":"no"}`},
	}
	for _, args := range commands {
		t.Run(args[0], func(t *testing.T) {
			_, err := runJarvisTools(t, "http://127.0.0.1:1", nil, args...)
			if err == nil || !strings.Contains(err.Error(), "does not accept summary") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestJarvisToolsUpdateGroupRequiresCompleteControlState(t *testing.T) {
	for _, payload := range []string{
		`{}`,
		`{"project_id":7,"related_group":true,"pinned":true,"is_key_group":true}`,
		`{"project_id":0,"related_group":true,"pinned":true,"include_in_memory":true,"is_key_group":true}`,
	} {
		_, err := runJarvisTools(t, "http://127.0.0.1:1", nil, "update-group", "--id", "8", "--payload", payload)
		if err == nil || !strings.Contains(err.Error(), "replaces the complete group control state") {
			t.Fatalf("payload=%s error=%v", payload, err)
		}
	}
}

func TestJarvisToolsGenericRelationCommandsAreDiscoverable(t *testing.T) {
	for _, command := range []string{"create-relation", "list-relations", "delete-relation"} {
		out, err := runJarvisTools(t, "", nil, command, "--help")
		if err != nil || !strings.Contains(out, "generic") {
			t.Fatalf("%s help = %q, error = %v", command, out, err)
		}
	}
	for _, command := range []string{"update-relation"} {
		_, err := runJarvisTools(t, "", nil, command, "--help")
		if err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
			t.Fatalf("%s help error = %v", command, err)
		}
	}
}

func TestJarvisToolsListRelationsReadsLargePaginatedResults(t *testing.T) {
	const pageSize = 200
	largeEvidence := strings.Repeat("完整业务 Ontology 关系证据", 128)
	firstPage := make([]map[string]any, pageSize)
	for index := range firstPage {
		firstPage[index] = map[string]any{
			"id": index + 2, "source_type": "okr_point", "source_id": fmt.Sprintf("p-%d", index),
			"relation_type": "owned_by", "target_type": "person", "target_id": fmt.Sprintf("%d", index+1),
			"evidence": map[string]any{"basis": largeEvidence},
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/relations" {
			t.Fatalf("request path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("node_types") != "okr_objective,okr_kr,okr_point" {
			t.Fatalf("node_types = %q", r.URL.Query().Get("node_types"))
		}
		if r.URL.Query().Get("cursor") == "2" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []map[string]any{{"id": 1, "source_type": "okr_point", "source_id": "last", "relation_type": "owned_by", "target_type": "person", "target_id": "201"}}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": firstPage, "next_cursor": "2"}})
	}))
	defer server.Close()

	output, err := runJarvisTools(t, server.URL, nil, "list-relations", "--node-types", "okr_objective,okr_kr,okr_point", "--limit", "200")
	if err != nil {
		t.Fatal(err)
	}
	var relations []map[string]any
	if err := json.Unmarshal([]byte(output), &relations); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if len(relations) != pageSize+1 {
		t.Fatalf("relation count = %d, want %d", len(relations), pageSize+1)
	}
}

func TestJarvisToolsSystemSourceDoesNotBorrowTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["source_kind"] != "system" || payload["source_id"] != nil {
			t.Fatalf("provenance = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"id":1}}`)
	}))
	defer server.Close()
	_, err := runJarvisTools(t, server.URL, []string{"JARVIS_TASK_ID=42"},
		"append-fact", "--subject-type", "project", "--subject-id", "1",
		"--description", "decision", "--source", "system")
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

func TestJarvisToolsCreateTaskAllowsEveryAgentStageAndRecordsCaller(t *testing.T) {
	var actors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/tasks" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		source, _ := payload["source_type"].(string)
		if source != "manual" && source != "proactive" {
			t.Fatalf("payload = %#v", payload)
		}
		actors = append(actors, payload["actor"].(string))
		if _, exists := payload["execution_mode"]; exists {
			t.Fatalf("payload still contains execution_mode: %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"code":0,"data":{"id":19,"source_type":%q,"status":"pending"}}`, source)
	}))
	defer server.Close()
	payload := `{"title":"推进阻塞","action_type":"agent_task","target":"完成目标","background":{"why_now":"条件已满足"},"source_payload":{"instruction":"完成目标"},"source_type":"proactive"}`
	out, err := runJarvisTools(t, server.URL, []string{"JARVIS_AGENT_STAGE=proactive"}, "create-task", "--payload", payload)
	if err != nil || !strings.Contains(out, `"id":19`) {
		t.Fatalf("output = %s, error = %v", out, err)
	}
	if _, err := runJarvisTools(t, server.URL, []string{"JARVIS_AGENT_STAGE=execute"}, "create-task", "--payload", payload); err != nil {
		t.Fatalf("create-task failed for execute stage: %v", err)
	}
	if _, err := runJarvisTools(t, server.URL, nil, "create-task", "--payload", payload); err != nil {
		t.Fatalf("create-task failed outside an Agent stage: %v", err)
	}
	manual := strings.Replace(payload, `"source_type":"proactive"`, `"source_type":"manual"`, 1)
	if _, err := runJarvisTools(t, server.URL, nil, "create-task", "--payload", manual); err != nil {
		t.Fatalf("create manual task: %v", err)
	}
	if !reflect.DeepEqual(actors, []string{"proactive", "m5", "user", "user"}) {
		t.Fatalf("actors = %#v", actors)
	}
}

func TestJarvisToolsDateUsesConfiguredTimezoneAndFailsBeforeRequest(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.URL.Query().Get("from"); got != "2026-03-08T00:00:00-05:00" {
			t.Fatalf("from = %q", got)
		}
		if got := r.URL.Query().Get("until"); got != "2026-03-09T00:00:00-04:00" {
			t.Fatalf("until = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"total":0,"page":1,"page_size":20,"items":[]}}`)
	}))
	defer server.Close()

	env := []string{"JARVIS_TIMEZONE=America/New_York"}
	if _, err := runJarvisTools(t, server.URL, env, "list-tasks", "--date", "2026-03-08"); err != nil {
		t.Fatalf("valid date failed: %v", err)
	}
	if requests != 1 {
		t.Fatalf("request count after valid date = %d", requests)
	}
	if _, err := runJarvisTools(t, server.URL, env, "list-tasks", "--date", "2026-02-30"); err == nil {
		t.Fatal("invalid calendar date succeeded")
	}
	if requests != 1 {
		t.Fatalf("invalid date sent an HTTP request; count = %d", requests)
	}
}

func TestJarvisToolsCanStartUpdateAndCloseTasksFromAnyStage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tasks/19/execute":
			if r.Method != http.MethodPost {
				t.Fatalf("start method = %s", r.Method)
			}
			fmt.Fprint(w, `{"code":0,"data":{"id":19,"status":"executing"}}`)
		case "/api/tasks/20/close":
			if r.Method != http.MethodPost {
				t.Fatalf("close method = %s", r.Method)
			}
			var payload struct {
				ExpectedVersion int            `json:"expected_version"`
				Result          map[string]any `json:"result"`
				ActorType       string         `json:"actor_type"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ExpectedVersion != 3 || payload.Result["evidence"] != "会议已结束" || payload.ActorType != "execute" {
				t.Fatalf("close payload = %#v", payload)
			}
			fmt.Fprint(w, `{"code":0,"data":{"id":20,"status":"done","resolution":{"actor_type":"execute"}}}`)
		case "/api/tasks/21":
			if r.Method != http.MethodPatch {
				t.Fatalf("update method = %s", r.Method)
			}
			var payload struct {
				ExpectedVersion int    `json:"expected_version"`
				Summary         string `json:"summary"`
				Instruction     string `json:"instruction"`
				Reason          string `json:"reason"`
				ActorType       string `json:"actor_type"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ExpectedVersion != 2 || payload.Summary == "" || payload.Instruction == "" || payload.Reason == "" || payload.ActorType != "execute" {
				t.Fatalf("update payload = %#v", payload)
			}
			fmt.Fprint(w, `{"code":0,"data":{"id":21,"status":"waiting","version":3}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	env := []string{"JARVIS_AGENT_STAGE=execute"}
	if out, err := runJarvisTools(t, server.URL, env, "start-task", "--id", "19"); err != nil || !strings.Contains(out, `"status":"executing"`) {
		t.Fatalf("start output = %s, error = %v", out, err)
	}
	payload := `{"expected_version":3,"result":{"summary":"过期关闭","evidence":"会议已结束"}}`
	if out, err := runJarvisTools(t, server.URL, env, "close-task", "--id", "20", "--payload", payload); err != nil || !strings.Contains(out, `"actor_type":"execute"`) {
		t.Fatalf("close output = %s, error = %v", out, err)
	}
	updatePayload := `{"expected_version":2,"summary":"权限仍在等待","instruction":"恢复后先核验权限","reason":"等待条件仍有效"}`
	if out, err := runJarvisTools(t, server.URL, env, "update-task", "--id", "21", "--payload", updatePayload); err != nil || !strings.Contains(out, `"version":3`) {
		t.Fatalf("update output = %s, error = %v", out, err)
	}
}

func runJarvisTools(t *testing.T, apiBase string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-tools"))
	if err != nil {
		t.Fatal(err)
	}
	cliArgs := []string{script}
	if apiBase != "" {
		cliArgs = append(cliArgs, "--api-base", apiBase)
	}
	command := exec.Command("bash", append(cliArgs, args...)...)
	command.Env = sanitizedEnv(command.Environ(), "JARVIS_API_BASE", "JARVIS_TASK_ID", "JARVIS_AGENT_STAGE", "JARVIS_TIMEZONE", "JARVIS_CONFIG_PATH", "JARVIS_DESKTOP", "JARVIS_RESOURCE_ROOT")
	command.Env = append(command.Env, extraEnv...)
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("jarvis-tools %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return string(output), nil
}

func sanitizedEnv(env []string, names ...string) []string {
	blocked := make(map[string]struct{}, len(names))
	for _, name := range names {
		blocked[name] = struct{}{}
	}
	clean := env[:0]
	for _, item := range env {
		name, _, ok := strings.Cut(item, "=")
		if ok {
			if _, exists := blocked[name]; exists {
				continue
			}
		}
		clean = append(clean, item)
	}
	return clean
}

func TestFrozenContextCLIUsesNativeMessageIDs(t *testing.T) {
	for _, test := range []struct {
		args             []string
		path, key, value string
	}{
		{[]string{"get-task", "--id", "9", "--message-id", "om_a/b"}, "/api/tasks/9", "message_id", "om_a/b"},
		{[]string{"get-todo", "--id", "9", "--context", "conversation"}, "/api/todos/9", "context", "conversation"},
		{[]string{"list-tasks", "--source-message-id", "om_source"}, "/api/tasks", "source_message_id", "om_source"},
		{[]string{"list-tasks", "--action-type", "delegated_followup"}, "/api/tasks", "action_type", "delegated_followup"},
		{[]string{"list-todos", "--source-message-id", "om_source"}, "/api/todos", "source_message_id", "om_source"},
	} {
		t.Run(strings.Join(test.args, "_"), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path || r.URL.Query().Get(test.key) != test.value {
					t.Errorf("unexpected URL: %s", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"code":0,"data":{"items":[],"context":{}}}`)
			}))
			defer server.Close()
			if _, err := runJarvisTools(t, server.URL, nil, test.args...); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := runJarvisTools(t, "", nil, "get-task", "--id", "9", "--material", "source"); err == nil {
		t.Fatal("retired material flag accepted")
	}
}

func TestDelegationToolsPreserveLooseProgressAndUseTodoIdentity(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/api/delegations":
			if r.URL.Query().Get("state") != "all" || r.URL.Query().Get("query") != "张三" || r.URL.Query().Get("page") != "2" {
				t.Errorf("query=%s", r.URL.RawQuery)
			}
		case "/api/delegations/7":
			if r.Method == "PATCH" {
				raw, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(raw), "9007199254740993") || !strings.Contains(string(raw), `"free":[1,"a"]`) {
					t.Errorf("changed JSON: %s", raw)
				}
			}
		case "/api/delegations/7/tasks":
			if r.URL.Query().Get("page") != "2" {
				t.Errorf("page=%s", r.URL.RawQuery)
			}
		default:
			t.Errorf("path=%s", r.URL.Path)
		}
		fmt.Fprint(w, `{"code":0,"data":{"items":[],"id":7}}`)
	}))
	defer server.Close()
	for _, args := range [][]string{
		{"list-delegations", "--state", "all", "--query", "张三", "--page", "2"},
		{"get-delegation", "--id", "7"},
		{"list-delegation-tasks", "--id", "7", "--page", "2"},
		{"update-delegation", "--id", "7", "--payload", `{"expected_version":0,"actor":"m5","content":{"proof":9007199254740993,"free":[1,"a"]},"closed":false}`},
	} {
		if out, err := runJarvisTools(t, server.URL, nil, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
}
