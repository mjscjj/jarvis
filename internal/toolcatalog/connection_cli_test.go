package toolcatalog

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func syntheticConnectionConfig(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "config", "runtime_settings_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(string(source), "const runtimeSettingsTestYAML = `")
	if !ok {
		t.Fatal("missing synthetic config")
	}
	body, _, ok = strings.Cut(body, "`")
	if !ok {
		t.Fatal("incomplete synthetic config")
	}
	return body
}

func TestConnectionCLIWorkspaceOverridesAndDesktop(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	// Exercise real config parsing without compiling on every fixture invocation.
	binary := filepath.Join(root, "jarvis-config")
	build := exec.Command("go", "build", "-o", binary, "./cmd/jarvis-config")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("config build: %v: %s", err, out)
	}
	for _, dir := range []string{"scripts", "conf", "bin", "desktop/bin"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, relative := range []string{"jarvis-tools", "json-api-data.mjs", "lib"} {
		source := filepath.Join(repo, "scripts", relative)
		err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(filepath.Join(repo, "scripts"), path)
			if err != nil {
				return err
			}
			target := filepath.Join(root, "scripts", rel)
			if entry.IsDir() {
				return os.MkdirAll(target, 0700)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, raw, 0700)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The source launcher must invoke go from the tool workspace, with the usual entrypoint.
	writeExecutable(t, filepath.Join(root, "bin", "go"), `#!/bin/sh
[ "$PWD" = "$FIXTURE_ROOT" ] && [ "$1" = run ] && [ "$2" = ./cmd/jarvis-config ] || exit 91
printf 'parsed\n' >> "$FIXTURE_ROOT/parses"
shift 2
exec "$FIXTURE_ROOT/jarvis-config" "$@"
`)
	if err := os.Symlink(binary, filepath.Join(root, "desktop/bin/jarvis-config")); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "conf", "config.yaml")
	if err := os.WriteFile(configPath, []byte(syntheticConnectionConfig(t)), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var dateStarts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/api/profile":
			fmt.Fprint(w, `{"code":0,"data":{"name":"isolated"}}`)
		case "/api/tasks":
			dateStarts = append(dateStarts, r.URL.Query().Get("from"))
			fmt.Fprint(w, `{"code":0,"data":{"total":0,"items":[]}}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	overlay := filepath.Join(root, "conf", "config.runtime.yaml")
	if err := os.WriteFile(overlay, []byte("server:\n  addr: '"+strings.Replace(strings.TrimPrefix(server.URL, "http://"), "127.0.0.1:", "0.0.0.0:", 1)+"'\ncapture:\n  timezone: Asia/Tokyo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "tools")
	if err := os.Symlink(filepath.Join(root, "scripts", "jarvis-tools"), link); err != nil {
		t.Fatal(err)
	}
	run := func(env []string, args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command("bash", append([]string{link}, args...)...)
		cmd.Dir = t.TempDir()
		cmd.Env = sanitizedEnv(os.Environ(), "JARVIS_API_BASE", "JARVIS_CONFIG_PATH", "JARVIS_TIMEZONE", "JARVIS_DESKTOP", "JARVIS_RESOURCE_ROOT")
		cmd.Env = append(cmd.Env, "FIXTURE_ROOT="+root, "PATH="+filepath.Join(root, "bin")+":"+os.Getenv("PATH"))
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for _, args := range [][]string{{"get-principal"}, {"--config", "conf/config.yaml", "get-principal"}, {"get-principal", "--config=conf/config.yaml"}, {"list-tasks", "--date", "2026-09-13"}} {
		out, err := run(nil, args...)
		if err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	parses, err := os.ReadFile(filepath.Join(root, "parses"))
	if err != nil || strings.Count(string(parses), "parsed") != 4 {
		t.Fatalf("want one parse per command: %s %v", parses, err)
	}
	for _, tc := range []struct{ env, args []string }{
		{[]string{"JARVIS_CONFIG_PATH=" + configPath}, []string{"get-principal"}},
		{[]string{"JARVIS_CONFIG_PATH=/missing"}, []string{"get-principal", "--config", configPath}},
		{[]string{"JARVIS_API_BASE=" + server.URL, "JARVIS_CONFIG_PATH=/missing"}, []string{"get-principal"}},
		{[]string{"JARVIS_API_BASE=http://127.0.0.1:1", "JARVIS_CONFIG_PATH=/missing"}, []string{"--api-base", server.URL, "get-principal"}},
		{[]string{"JARVIS_API_BASE=" + server.URL, "JARVIS_TIMEZONE=UTC", "JARVIS_CONFIG_PATH=/missing"}, []string{"list-tasks", "--date", "2026-09-13"}},
		{[]string{"JARVIS_DESKTOP=1", "JARVIS_RESOURCE_ROOT=" + filepath.Join(root, "desktop")}, []string{"get-principal"}},
	} {
		out, err := run(tc.env, tc.args...)
		if err != nil {
			t.Fatalf("%v: %v: %s", tc.args, err, out)
		}
	}
	parses, _ = os.ReadFile(filepath.Join(root, "parses"))
	if strings.Count(string(parses), "parsed") != 6 {
		t.Fatalf("explicit connection or desktop invoked go: %s", parses)
	}
	if len(dateStarts) != 2 || dateStarts[0] != "2026-09-13T00:00:00+09:00" || dateStarts[1] != "2026-09-13T00:00:00+00:00" {
		t.Fatalf("wrong date timezone: %v", dateStarts)
	}
	before := calls
	for _, tc := range []struct{ env, args []string }{
		{nil, []string{"get-principal", "--api-base=http://127.0.0.1:1"}},
		{nil, []string{"get-principal", "--api-base=invalid"}},
		{nil, []string{"get-principal", "--config=/missing"}},
		{[]string{"JARVIS_API_BASE=invalid"}, []string{"get-principal"}},
		{[]string{"JARVIS_DESKTOP=1", "JARVIS_RESOURCE_ROOT=/missing"}, []string{"get-principal"}},
	} {
		out, err := run(tc.env, tc.args...)
		if err == nil {
			t.Fatalf("accepted %v: %s", tc.args, out)
		}
	}
	if calls != before {
		t.Fatalf("failed connection switched to config: %d -> %d", before, calls)
	}
	if err := os.WriteFile(overlay, []byte("server: [invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(nil, "get-principal"); err == nil {
		t.Fatalf("invalid overlay accepted: %s", out)
	}
}

func TestRebuildChecksSelectedInstanceBeforeRestart(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skip("shell unavailable")
			}
			root := t.TempDir()
			for _, dir := range []string{"scripts/lib", "bin"} {
				if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			name := "rebuild-server-linux.sh"
			if shell == "zsh" {
				name = "rebuild-server.sh"
			}
			for _, rel := range []string{name, "jarvis-deploy", "lib/connection.sh", "lib/retire-chat-sidecar.sh"} {
				raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", rel))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "scripts", rel), raw, 0700); err != nil {
					t.Fatal(err)
				}
			}
			platform := "Linux"
			if shell == "zsh" {
				platform = "Darwin"
			}
			writeExecutable(t, filepath.Join(root, "bin", "uname"), "#!/bin/sh\nprintf '"+platform+"\\n'\n")
			for _, helper := range []string{"check-build-toolchain.sh", "sign-jarvis-server.sh", "verify-server-signature.sh"} {
				writeExecutable(t, filepath.Join(root, "scripts", helper), "#!/bin/sh\nexit 0\n")
			}
			writeExecutable(t, filepath.Join(root, "scripts", "jarvis-instance"), `#!/bin/sh
printf '%s\n' '{"config_path":"/fixture/config.yaml","api_base":"http://127.0.0.1:18847","launchd_label":"com.bytedance.jarvis.server.fixture","log_files":["var/log/out","var/log/err"]}'
`)
			writeExecutable(t, filepath.Join(root, "bin", "npm"), "#!/bin/sh\nexit 0\n")
			writeExecutable(t, filepath.Join(root, "bin", "lsof"), "#!/bin/sh\nprintf 'n*:18847\\n'\n")
			for _, name := range []string{"systemctl", "launchctl"} {
				writeExecutable(t, filepath.Join(root, "bin", name), `#!/bin/sh
case "$*" in *restart*|*kickstart*|*bootstrap*) exit 99 ;; *print*) printf 'pid = 123\n' ;; esac
exit 0
`)
			}
			writeExecutable(t, filepath.Join(root, "bin", "go"), `#!/bin/sh
case "$*" in
  build*|*migrate-retired-config*|*okr-chat-image*) exit 0 ;;
  *show-connection*) printf '%s\n' '{"api_base":"http://127.0.0.1:18847","timezone":"UTC"}' ;;
  *) exit 99 ;;
esac
`)
			writeExecutable(t, filepath.Join(root, "bin", "curl"), `#!/bin/sh
printf '%s\n' "$*" >> "$FIXTURE_ROOT/requests"
printf '%s\n' '{"code":0,"data":{"total":1}}'
`)
			args := []string{filepath.Join(root, "scripts", name)}
			if shell == "bash" {
				args = append(args, "--skip-pull")
			}
			cmd := exec.Command(shell, args...)
			cmd.Env = sanitizedEnv(os.Environ(), "JARVIS_API_BASE", "JARVIS_CONFIG_PATH", "JARVIS_DESKTOP", "JARVIS_RESOURCE_ROOT")
			cmd.Env = append(cmd.Env, "PATH="+filepath.Join(root, "bin")+":"+os.Getenv("PATH"), "FIXTURE_ROOT="+root, "HOME="+root)
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "refusing to restart") {
				t.Fatalf("expected running Task refusal: %v %s", err, out)
			}
			requests, err := os.ReadFile(filepath.Join(root, "requests"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(requests), "http://127.0.0.1:18847/api/tasks?") || strings.Contains(string(requests), ":18800") {
				t.Fatalf("wrong instance: %s", requests)
			}
		})
	}
}
