package toolcatalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJarvisWorldModelOwnsWorldModelRunStateOnly(t *testing.T) {
	help, err := runJarvisWorldModel(t, "", nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"preflight", "discover", "scan", "validate", "does not install services", "installation state", "configure CC Connect"} {
		if !strings.Contains(help, want) {
			t.Fatalf("jarvis-world-model help missing %q:\n%s", want, help)
		}
	}
	for _, forbidden := range []string{"\n  start      ", "\n  status     ", "configure-app", "set-cc-app-secret", "validate-binding", "install-server"} {
		if strings.Contains(help, forbidden) {
			t.Fatalf("jarvis-world-model still owns install concern %q:\n%s", forbidden, help)
		}
	}
	toolsHelp, err := runJarvisTools(t, "", nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"validate-initialization", "discover-chats", "scan-chat"} {
		if strings.Contains(toolsHelp, forbidden) {
			t.Fatalf("jarvis-tools exposes initialization-only command %q:\n%s", forbidden, toolsHelp)
		}
	}
}

func TestJarvisWorldModelValidateReportsWorldModelWithoutRequiringGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/profile":
			fmt.Fprint(w, `{"code":0,"data":{"open_id":"ou_ready","name":"Ready User","leader_open_id":"ou_leader","saved":true}}`)
		case "/api/projects":
			fmt.Fprint(w, `{"code":0,"data":{"total":2,"items":[]}}`)
		case "/api/persons":
			fmt.Fprint(w, `{"code":0,"data":{"total":3,"items":[]}}`)
		case "/api/key-matters":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"items":[]}}`)
		case "/api/resources":
			fmt.Fprint(w, `{"code":0,"data":{"total":4,"active_total":3,"items":[]}}`)
		case "/api/relation-facts":
			fmt.Fprint(w, `{"code":0,"data":{"total":5,"items":[]}}`)
		case "/api/groups":
			fmt.Fprint(w, `{"code":0,"data":{"total":0,"items":[]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
case "$*" in
  "run ./cmd/jarvis-config show-principal --config conf/config.yaml")
    printf '%s' '{"principal_open_id":"ou_ready","lark_profile":"cli_ready","git_author":"ready@example.com"}' ;;
  *) printf '%s' "unexpected go args: $*" >&2; exit 9 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  printf '%s' '{"identity":"user","verified":true,"identities":{"bot":{"status":"ready","verified":true},"user":{"status":"ready","verified":true,"tokenStatus":"valid","openId":"ou_ready","userName":"Ready User"}}}'
  exit 0
fi
printf '%s\n' "unexpected lark-cli args: $*" >&2
exit 9
`)
	out, err := runJarvisWorldModel(t, server.URL, []string{"PATH=" + binDir + ":" + os.Getenv("PATH")}, "validate", "--profile", "cli_ready")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Ready        bool `json:"ready"`
		Observations struct {
			RelatedGroup bool `json:"related_group_configured"`
		} `json:"observations"`
		Counts struct {
			Projects  int `json:"projects"`
			Resources int `json:"resources"`
			Relations int `json:"relations"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if !result.Ready || result.Observations.RelatedGroup || result.Counts.Projects != 2 || result.Counts.Resources != 4 || result.Counts.Relations != 5 {
		t.Fatalf("validation result = %#v", result)
	}
}

func TestJarvisWorldModelCaptureCommandsReuseM2Endpoints(t *testing.T) {
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/debug/capture/discover":
			fmt.Fprint(w, `{"code":0,"data":{"action":"discover","ok":true}}`)
		case "/api/debug/capture/scan-chat":
			var payload struct {
				ChatID string `json:"chat_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ChatID != "oc_init" {
				t.Fatalf("scan payload = %#v", payload)
			}
			fmt.Fprint(w, `{"code":0,"data":{"action":"scan_chat","chat_id":"oc_init","ok":true}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, err := runJarvisWorldModel(t, server.URL, nil, "discover"); err != nil {
		t.Fatal(err)
	}
	if _, err := runJarvisWorldModel(t, server.URL, nil, "scan", "--chat-id", "oc_init"); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /api/debug/capture/discover", "POST /api/debug/capture/scan-chat"}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}
}

func TestJarvisWorldModelCaptureFailureIsNotSwallowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"code":50030,"message":"discover failed"}`)
	}))
	defer server.Close()
	_, err := runJarvisWorldModel(t, server.URL, nil, "discover")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("discover error = %v, want HTTP 500", err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func runJarvisWorldModel(t *testing.T, apiBase string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-world-model"))
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
		return string(output), fmt.Errorf("jarvis-world-model %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return string(output), nil
}
