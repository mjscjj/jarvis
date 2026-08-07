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

func TestJarvisInitIsSkillLocalAndAbsentFromJarvisTools(t *testing.T) {
	initHelp, err := runJarvisInit(t, "", nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"private to $initialize-jarvis", "configure", "discover", "scan", "validate"} {
		if !strings.Contains(initHelp, want) {
			t.Fatalf("jarvis-init help missing %q:\n%s", want, initHelp)
		}
	}
	toolsHelp, err := runJarvisTools(t, "", nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"configure-principal", "validate-initialization", "discover-chats", "scan-chat"} {
		if strings.Contains(toolsHelp, forbidden) {
			t.Fatalf("jarvis-tools still exposes initialization command %q:\n%s", forbidden, toolsHelp)
		}
	}
}

func TestJarvisInitConfigureDelegatesToConfigBoundary(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
case "$*" in
  "run ./cmd/jarvis-config configure-principal --config conf/config.yaml --open-id ou_ready --profile cli_ready")
    printf '%s' '{"runtime_config_path":"conf/config.runtime.yaml","principal_open_id":"ou_ready","lark_profile":"cli_ready","restart_required":true}' ;;
  *) printf '%s' "unexpected go args: $*" >&2; exit 9 ;;
esac
`)
	out, err := runJarvisInit(t, "", []string{"PATH=" + binDir + ":" + os.Getenv("PATH")},
		"configure", "--open-id", "ou_ready", "--profile", "cli_ready")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"principal_open_id":"ou_ready"`) {
		t.Fatalf("configure output = %s", out)
	}
}

func TestJarvisInitValidate(t *testing.T) {
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
		case "/api/groups":
			fmt.Fprint(w, `{"code":0,"data":{"total":1,"items":[{"chat_id":"oc_ready","last_scan_status":"ok"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
printf '%s' '{"identity":"user","verified":true,"identities":{"user":{"status":"ready","verified":true,"tokenStatus":"valid","openId":"ou_ready","userName":"Ready User"}}}'
`)
	out, err := runJarvisInit(t, server.URL, []string{"PATH=" + binDir + ":" + os.Getenv("PATH")},
		"validate", "--profile", "cli_ready")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Ready  bool `json:"ready"`
		Counts struct {
			Projects      int `json:"projects"`
			RelatedGroups int `json:"related_groups"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if !result.Ready || result.Counts.Projects != 2 || result.Counts.RelatedGroups != 1 {
		t.Fatalf("validation result = %#v", result)
	}
}

func TestJarvisInitCaptureCommandsReuseM2Endpoints(t *testing.T) {
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

	if _, err := runJarvisInit(t, server.URL, nil, "discover"); err != nil {
		t.Fatal(err)
	}
	if _, err := runJarvisInit(t, server.URL, nil, "scan", "--chat-id", "oc_init"); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /api/debug/capture/discover", "POST /api/debug/capture/scan-chat"}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}
}

func TestJarvisInitCaptureFailureIsNotSwallowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"code":50030,"message":"discover failed"}`)
	}))
	defer server.Close()
	_, err := runJarvisInit(t, server.URL, nil, "discover")
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

func runJarvisInit(t *testing.T, apiBase string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", ".agents", "skills", "initialize-jarvis", "scripts", "jarvis-init"))
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
		return string(output), fmt.Errorf("jarvis-init %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return string(output), nil
}
