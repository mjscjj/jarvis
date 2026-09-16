package toolcatalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestJarvisToolsAllHelpIsOfflineAndGrouped(t *testing.T) {
	// Only path-resolution/help utilities exist: no curl, jq or node.
	path := t.TempDir()
	for _, name := range []string{"dirname", "readlink", "cat"} {
		binary, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(binary, filepath.Join(path, name)); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"PATH=" + path}
	top, err := runJarvisTools(t, "", env, "--help")
	if err != nil {
		t.Fatal(err)
	}
	groups := []string{"world", "evidence", "task", "schedule", "memory", "skill", "notify"}
	for _, group := range groups {
		if !strings.Contains(top, "  "+group+" ") {
			t.Fatalf("top help missing %s: %s", group, top)
		}
	}
	if strings.Contains(top, "list-projects") || !strings.Contains(top, "how-to:") {
		t.Fatalf("top help not progressive: %s", top)
	}
	all, err := runJarvisTools(t, "", env, "help", "all")
	if err != nil {
		t.Fatal(err)
	}
	commandLine := regexp.MustCompile(`(?m)^  ([a-z]+(?:-[a-z]+)+)\s+`)
	commands := commandLine.FindAllStringSubmatch(all, -1)
	if len(commands) != 94 {
		t.Fatalf("commands = %d, want 94 merged commands", len(commands))
	}
	seen := map[string]int{}
	for _, group := range groups {
		help, err := runJarvisTools(t, "", env, "help", group)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range commandLine.FindAllStringSubmatch(help, -1) {
			seen[match[1]]++
		}
	}
	for _, match := range commands {
		name := match[1]
		if seen[name] != 1 {
			t.Fatalf("%s appears in %d groups", name, seen[name])
		}
		help, err := runJarvisTools(t, "", env, name, "--help")
		if err != nil || !strings.Contains(help, "usage: jarvis-tools ") || strings.Contains(help, "--config") {
			t.Fatalf("%s help=%s err=%v", name, help, err)
		}
	}
	if _, err := runJarvisTools(t, "", env, "help", "unknown"); err == nil {
		t.Fatal("unknown help group accepted")
	}
}

func TestJarvisToolsWorldListsUseServerFiltersAndSecondPage(t *testing.T) {
	for _, test := range []struct {
		args      []string
		path, key string
		filters   map[string]string
	}{
		{[]string{"list-projects", "--code", "A/B"}, "/api/projects", "projects", map[string]string{"code": "A/B"}},
		{[]string{"list-persons", "--role", "key", "--open-id", "ou_exact"}, "/api/persons", "persons", map[string]string{"role": "key", "open_id": "ou_exact"}},
		{[]string{"list-key-matters", "--all"}, "/api/key-matters", "key_matters", map[string]string{"include_closed": "true"}},
		{[]string{"list-groups", "--chat-id", "oc_exact"}, "/api/groups", "items", map[string]string{"chat_id": "oc_exact"}},
		{[]string{"query-resources", "--person-open-id", "ou_missing", "--project-id", "7", "--principal-only"}, "/api/resources", "resources", map[string]string{"person_open_id": "ou_missing", "project_id": "7", "principal_only": "true", "active_only": "true"}},
	} {
		t.Run(test.args[0], func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != test.path || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("page_size") != "3" || r.URL.Query().Get("keyword") != "状态 & 50%_" {
					t.Errorf("query=%s", r.URL)
				}
				for key, want := range test.filters {
					if r.URL.Query().Get(key) != want {
						t.Errorf("%s=%q want %q", key, r.URL.Query().Get(key), want)
					}
				}
				fmt.Fprint(w, `{"code":0,"data":{"total":8,"page":2,"page_size":3,"items":[{"id":4,"name":"server-selected","title":"server-selected","project":null}]}}`)
			}))
			defer server.Close()
			args := append(test.args, "--keyword", "状态 & 50%_", "--page", "2", "--limit", "3")
			out, err := runJarvisTools(t, server.URL, nil, args...)
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]json.RawMessage
			if err := json.Unmarshal([]byte(out), &data); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || string(data["page"]) != "2" || string(data["page_size"]) != "3" || string(data["total"]) != "8" || !bytes.Contains(data[test.key], []byte(`"id":4`)) {
				t.Fatalf("calls=%d output=%s", calls, out)
			}
		})
	}
}

func TestJarvisToolsExactWorldLookupChecksTotal(t *testing.T) {
	for _, test := range []struct{ command, flag, field, path string }{
		{"get-project", "--code", "code", "/api/projects"},
		{"get-person", "--open-id", "open_id", "/api/persons"},
		{"get-group", "--chat-id", "chat_id", "/api/groups"},
	} {
		for _, total := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("%s-%d", test.command, total), func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.URL.Path != test.path || r.URL.Query().Get(test.field) != "exact /& value" || r.URL.Query().Has("keyword") || r.URL.Query().Get("page_size") != "1" {
						t.Errorf("query=%s", r.URL)
					}
					if test.command == "get-group" && r.URL.Query().Get("related_only") != "false" {
						t.Errorf("unmonitored group excluded: %s", r.URL)
					}
					fmt.Fprintf(w, `{"code":0,"data":{"total":%d,"items":[{"id":9007199254740993,%q:"exact /& value","extra":{"number":1e400}}]}}`, total, test.field)
				}))
				defer server.Close()
				out, err := runJarvisTools(t, server.URL, nil, test.command, test.flag, "exact /& value")
				if total == 1 {
					if err != nil || !strings.Contains(out, "9007199254740993") || !strings.Contains(out, "1e400") {
						t.Fatalf("out=%s err=%v", out, err)
					}
				} else if err == nil {
					t.Fatalf("total=%d succeeded: %s", total, out)
				}
				if calls != 1 {
					t.Fatalf("calls=%d", calls)
				}
			})
		}
	}
}

func TestJarvisToolsExplicitAddressIdentityAndTimezone(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/agent-identity" {
			t.Errorf("path=%s", r.URL.Path)
		}
		fmt.Fprint(w, `{"code":0,"data":{"display_name":"Jarvis","principal_open_id":"ou_me"}}`)
	}))
	defer server.Close()
	for _, test := range []struct {
		base      string
		env, args []string
	}{
		{server.URL, []string{"JARVIS_API_BASE=http://127.0.0.1:1"}, []string{"get-agent-identity"}},
		{"", []string{"JARVIS_API_BASE=" + server.URL}, []string{"get-agent-identity"}},
		{"", []string{"JARVIS_API_BASE=http://127.0.0.1:1"}, []string{"get-agent-identity", "--api-base=" + server.URL + "/"}},
	} {
		out, err := runJarvisTools(t, test.base, test.env, test.args...)
		if err != nil || !strings.Contains(out, `"principal_open_id":"ou_me"`) {
			t.Fatalf("out=%s err=%v", out, err)
		}
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
	for _, args := range [][]string{{"get-principal"}, {"get-principal", "--config", "/missing/config.yaml"}, {"get-principal", "--api-base="}, {"get-principal", "--config="}} {
		if _, err := runJarvisTools(t, "", []string{"JARVIS_CONFIG_PATH=/missing/config.yaml"}, args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, command := range []string{"list-tasks", "list-todos", "query-messages", "list-facts"} {
		args := []string{command, "--date", "2026-09-13"}
		if command == "list-facts" {
			args = append(args, "--subject-type", "project", "--subject-id", "7")
		}
		_, err := runJarvisTools(t, server.URL, []string{"JARVIS_CONFIG_PATH=/missing/config.yaml"}, args...)
		if err == nil || !strings.Contains(err.Error(), "/missing/config.yaml") {
			t.Fatalf("%s err=%v", command, err)
		}
	}
	if calls != 3 {
		t.Fatalf("invalid date config caused HTTP requests: %d", calls)
	}
}

func TestJarvisToolsTransportErrorsPreserveRawBodyAndExit(t *testing.T) {
	const raw = `{"code":500,"message":"readback failed","data":{"message_id":"om_sent","send_response":{"id":9007199254740993,"decimal":0.123456789012345678901,"future":1e400}}}`
	for _, test := range []struct {
		name   string
		status int
		body   string
		args   []string
		ok     bool
	}{
		{"204", 204, "", []string{"delete-person", "--id", "7"}, true},
		{"404", 404, raw, []string{"get-principal"}, false},
		{"API404", 200, `{"code":40420,"data":null}`, []string{"get-principal"}, false},
		{"invalid", 200, "not json", []string{"get-principal"}, false},
		{"missing data", 200, `{"code":0}`, []string{"get-principal"}, false},
		{"array envelope", 200, `[]`, []string{"get-principal"}, false},
		{"API error", 200, raw, []string{"get-principal"}, false},
		{"conflict", 409, raw, []string{"update-page", "--type", "project", "--id", "7", "--content", "new", "--if-unchanged-since", "old"}, false},
		{"partial notice", 500, raw, []string{"notice-principal", "--payload", `{"content":"notice","idempotency_key":"stable"}`}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			script, _ := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-tools"))
			command := exec.Command("bash", append([]string{script, "--api-base", server.URL}, test.args...)...)
			command.Env = sanitizedEnv(command.Environ(), "JARVIS_TASK_ID", "JARVIS_API_BASE")
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			err := command.Run()
			if test.ok {
				if err != nil || strings.TrimSpace(stdout.String()) != "null" || stderr.Len() != 0 {
					t.Fatalf("out=%s err=%v stderr=%s", &stdout, err, &stderr)
				}
			} else {
				if err == nil || stdout.Len() != 0 || stderr.Len() == 0 {
					t.Fatalf("out=%s err=%v stderr=%s", &stdout, err, &stderr)
				}
				if test.body == raw && !strings.Contains(stderr.String(), raw) {
					t.Fatalf("lost raw receipt: %s", &stderr)
				}
			}
			if calls != 1 {
				t.Fatalf("transport retried: %d", calls)
			}
		})
	}
}

func TestJarvisToolsLooseJSONReadsWritesAndContextStayExact(t *testing.T) {
	const data = `{"id":9007199254740993,"decimal":0.123456789012345678901,"future":{"value":1e400},"text":"a } , \\\"data\\\": b"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "numeric_proof") && (!bytes.Contains(body, []byte("9007199254740993")) || !bytes.Contains(body, []byte("1e400"))) {
				t.Errorf("write lost JSON: %s", body)
			}
		}
		fmt.Fprint(w, `{"meta":{},"data":`+data+`,"code":0}`)
	}))
	defer server.Close()
	for _, args := range [][]string{
		{"get-principal"}, {"get-resource", "--id", "7"}, {"get-message", "--id", "7"}, {"get-todo-event", "--id", "7"}, {"get-task-event", "--id", "7"}, {"get-captured-resource", "--id", "7"}, {"get-scheduled-task", "--id", "7"}, {"get-page", "--type", "project", "--id", "7"}, {"get-skill", "--name", "x"}, {"query-messages"}, {"list-facts", "--subject-type", "project", "--subject-id", "7"},
		{"update-delegation", "--id", "7", "--payload", `{"content":{"numeric_proof":9007199254740993,"future":1e400}}`},
		{"notice-principal", "--payload", `{"content":"notice","idempotency_key":"stable","extra":{"numeric_proof":9007199254740993,"future":1e400}}`},
		{"close-task", "--id", "7", "--payload", `{"expected_version":0,"result":{"summary":"done","numeric_proof":9007199254740993,"future":1e400}}`},
		{"update-task", "--id", "7", "--payload", `{"expected_version":0,"summary":"working","reason":"new evidence","numeric_proof":9007199254740993,"future":1e400}`},
	} {
		out, err := runJarvisTools(t, server.URL, []string{"JARVIS_TASK_ID=7", "JARVIS_AGENT_STAGE=execute"}, args...)
		if err != nil || strings.TrimSpace(out) != data {
			t.Fatalf("%v changed data: %s err=%v", args, out, err)
		}
	}
	out, err := runJarvisTools(t, server.URL, nil, "get-context")
	if err != nil || strings.TrimSpace(out) != strings.TrimSuffix(data, "}")+`,"agent_identity":`+data+`}` {
		t.Fatalf("context changed JSON: %s err=%v", out, err)
	}
}

func TestJarvisToolsRejectInvalidInputBeforeHTTP(t *testing.T) {
	for _, args := range [][]string{
		{"create-project", "--payload", "[]"}, {"update-person", "--id", "0", "--payload", "{}"}, {"create-resource", "--payload", `{"summary":"wrong"}`},
		{"list-projects", "--page", "0"}, {"list-persons", "--limit", "101"}, {"list-key-matters", "--page", "x"}, {"query-resources", "--project-id", "-1"}, {"list-groups", "--page", "-1"},
		{"get-project", "--id", "7", "--code", "x"}, {"get-project"}, {"get-person"}, {"get-group"}, {"get-principal", "--payload", "{}"}, {"get-principal", "--unknown"}, {"get-principal", "--run-limit", "1"}, {"list-projects", "--keyword"},
	} {
		out, err := runJarvisTools(t, "http://127.0.0.1:1", nil, args...)
		if err == nil || strings.Contains(out, "curl") {
			t.Fatalf("%v should fail before HTTP: out=%s err=%v", args, out, err)
		}
	}
}
