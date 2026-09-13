package toolcatalog

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJarvisInstallChecklistCanResume(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(repoRoot, "var", "install", fmt.Sprintf("test-%d", time.Now().UnixNano()))
	defer os.RemoveAll(runDir)

	out, err := runJarvisInstall(t, nil, "start", "--run-dir", runDir)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		OK        bool   `json:"ok"`
		RunDir    string `json:"run_dir"`
		Checklist string `json:"checklist"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	if !created.OK || created.RunDir != runDir {
		t.Fatalf("start result = %#v", created)
	}
	if info, err := os.Stat(created.Checklist); err != nil || info.Size() == 0 {
		t.Fatalf("checklist = %q: %v", created.Checklist, err)
	}

	status, err := runJarvisInstall(t, nil, "status", "--run-dir", runDir)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Pending  int  `json:"pending"`
		Complete bool `json:"complete"`
	}
	if err := json.Unmarshal([]byte(status), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Pending == 0 || summary.Complete {
		t.Fatalf("status = %#v", summary)
	}

	resumed, err := runJarvisInstall(t, nil, "start", "--resume-latest")
	if err != nil {
		t.Fatal(err)
	}
	var resumedRun struct {
		Resumed   bool   `json:"resumed"`
		Checklist string `json:"checklist"`
	}
	if err := json.Unmarshal([]byte(resumed), &resumedRun); err != nil {
		t.Fatal(err)
	}
	if !resumedRun.Resumed || resumedRun.Checklist != created.Checklist {
		t.Fatalf("resumed run = %#v", resumedRun)
	}
}

func TestJarvisInstallDoctorKeepsCommandFailureInJSON(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
case "$*" in
  "env GOVERSION") printf '%s\n' 'go1.26.4' ;;
  "env CGO_ENABLED") printf '%s\n' '1' ;;
  "env CC") printf '%s\n' 'cc' ;;
  *"run ./cmd/jarvis-config initialization-status"*)
    printf '%s\n' 'configuration failed' >&2
    exit 7 ;;
  *) exit 9 ;;
esac
`)
	for _, name := range []string{"lark-cli", "bytedcli", "codex", "traex", "systemctl", "launchctl"} {
		writeExecutable(t, filepath.Join(binDir, name), "#!/bin/sh\nexit 1\n")
	}
	requestLog := filepath.Join(binDir, "requests")
	writeExecutable(t, filepath.Join(binDir, "curl"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CURL_LOG\"\nexit 1\n")
	t.Setenv("CURL_LOG", requestLog)

	output, err := runJarvisInstall(t, []string{"PATH=" + binDir + ":" + os.Getenv("PATH")}, "doctor")
	if err == nil {
		t.Fatalf("doctor unexpectedly reported ready: %s", output)
	}
	var report struct {
		Configuration struct {
			InspectionOK bool   `json:"inspection_ok"`
			Error        string `json:"error"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("doctor output is not JSON %q: %v", output, err)
	}
	var connectionReport struct {
		Services struct {
			Server struct {
				Healthy *bool   `json:"healthy"`
				APIBase *string `json:"api_base"`
				Error   string  `json:"connection_error"`
			} `json:"server"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(output), &connectionReport); err != nil {
		t.Fatal(err)
	}
	if connectionReport.Services.Server.Healthy != nil || connectionReport.Services.Server.APIBase != nil || connectionReport.Services.Server.Error == "" {
		t.Fatalf("missing unknown connection: %#v", connectionReport)
	}
	requests, err := os.ReadFile(requestLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(requests), ":18800") {
		t.Fatalf("probed default address: %s", requests)
	}

	if report.Configuration.InspectionOK || !strings.Contains(report.Configuration.Error, "configuration failed") {
		t.Fatalf("configuration report = %#v", report.Configuration)
	}
}

func TestJarvisInstallConfiguresIdentityThroughMachineBoundary(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
case "$*" in
  *"run ./cmd/jarvis-config configure-principal"*"--agent-name 小贾 --open-id ou_ready --git-author ready@example.com")
    printf '%s' '{"agent_display_name":"小贾","principal_open_id":"ou_ready","git_author":"ready@example.com"}' ;;
  *) exit 9 ;;
esac
`)
	out, err := runJarvisInstall(t, []string{"PATH=" + binDir + ":" + os.Getenv("PATH")},
		"configure-identity", "--agent-name", "小贾", "--open-id", "ou_ready", "--git-author", "ready@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		AgentName string `json:"agent_display_name"`
		OpenID    string `json:"principal_open_id"`
	}
	if err := json.Unmarshal([]byte(out), &identity); err != nil {
		t.Fatal(err)
	}
	if identity.AgentName != "小贾" || identity.OpenID != "ou_ready" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestJarvisInstallRefusesToReplaceLoadedServer(t *testing.T) {
	binDir := t.TempDir()
	for _, commandName := range []string{"curl", "go", "npm", "lark-cli", "traex"} {
		writeExecutable(t, filepath.Join(binDir, commandName), "#!/bin/sh\nexit 0\n")
	}
	writeExecutable(t, filepath.Join(binDir, "launchctl"), "#!/bin/sh\nexit 0\n")

	output, err := runJarvisInstall(t, []string{"PATH=" + binDir + ":" + os.Getenv("PATH")}, "install-server")
	if err == nil || !strings.Contains(output, "already loaded") {
		t.Fatalf("install-server replaced loaded service: %v: %s", err, output)
	}
}

func TestCCBindWritesEffectiveConnectionForIndependentDaemons(t *testing.T) {
	// Everything is isolated, including HOME, config, fake credentials and all
	// commands that could reach a network. This does not run an installer.
	for _, platform := range []string{"Darwin", "Linux"} {
		t.Run(platform, func(t *testing.T) {
			repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			binDir := filepath.Join(root, "bin")
			if err := os.Mkdir(binDir, 0o700); err != nil {
				t.Fatal(err)
			}
			ccPath := filepath.Join(root, "cc.toml")
			apiBase := "http://[::1]:18842"
			identity := fmt.Sprintf(`{"principal_open_id":"ou_fixture","card_approval_enabled":true,"card_approval_principal_open_id":"ou_fixture","relay_secret":"fixture-relay","relay_secret_sha256":"%x"}`, sha256.Sum256([]byte("fixture-relay")))
			writeExecutable(t, filepath.Join(binDir, "go"), "#!/bin/sh\ncase \"$*\" in\n*show-principal*) printf '%s\\n' '"+identity+"' ;;\n*show-connection*) [ \"$7\" = \"[::1]:18842\" ] || exit 8; printf '%s\\n' '{\"api_base\":\""+apiBase+"\",\"timezone\":\"Asia/Tokyo\"}' ;;\n*) exit 9 ;;\nesac\n")
			writeExecutable(t, filepath.Join(binDir, "uname"), "#!/bin/sh\nprintf '%s\\n' '"+platform+"'\n")
			writeExecutable(t, filepath.Join(binDir, "curl"), "#!/bin/sh\nprintf '%s\\n' '{\"code\":0,\"tenant_access_token\":\"fixture-token\"}'\n")
			writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
case "$*" in
  'config show') printf '%s\n' '{"appId":"fixture-app","brand":"feishu"}' ;;
  'auth status --json --verify') printf '%s\n' '{"verified":true,"identities":{"user":{"status":"ready","verified":true,"tokenStatus":"valid","openId":"ou_fixture"},"bot":{"status":"ready","verified":true}}}' ;;
  'event consume card.action.trigger --as bot --dry-run') printf '%s\n' '{"ok":true,"data":{"decision":{"event_key":"card.action.trigger","identity":"bot","status":"ready","preconditions":[{"name":"console_event_published","status":"ok"},{"name":"scopes_granted","status":"ok"}]}}}' ;;
  *) exit 9 ;;
esac
`)
			env := append(os.Environ(), "HOME="+root, "PATH="+binDir+":"+os.Getenv("PATH"), "JARVIS_API_BASE=http://stale-parent:1", "JARVIS_TIMEZONE=Asia/Tokyo")
			runManage := func(args ...string) string {
				t.Helper()
				wrapperArgs := append([]string(nil), args...)
				if wrapperArgs[0] == "bind" {
					wrapperArgs[0] = "bind-cc"
				} else {
					wrapperArgs[0] = "validate-binding"
				}
				cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot, "scripts", "jarvis-install")}, wrapperArgs...)...)
				cmd.Env = env
				cmd.Stdin = strings.NewReader("fixture-app-secret\n")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("CC manage failed: %v\n%s", err, out)
				}
				return string(out)
			}
			args := []string{"bind", "--config", filepath.Join(root, "unused-mock.yaml"), "--cc-config", ccPath, "--addr", "[::1]:18842"}
			out := runManage(args...)
			if !strings.Contains(out, `"agent_connection_matches_effective_config":true`) {
				t.Fatalf("missing connection validation: %s", out)
			}
			raw, err := os.ReadFile(ccPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"[projects.agent.options.env]\nJARVIS_API_BASE = \"" + apiBase + "\"\nJARVIS_TIMEZONE = \"Asia/Tokyo\"",
				apiBase + "/internal/card-approval/callback",
				apiBase + "/internal/message-routing/claim",
				apiBase + "/internal/meeting-sweep/wake",
			} {
				if !strings.Contains(string(raw), want) {
					t.Fatalf("CC config missing %q", want)
				}
			}
			// Existing env values are corrected without losing other env keys or
			// touching a separate project; rebind remains idempotent.
			changed := strings.ReplaceAll(string(raw), `JARVIS_TIMEZONE = "Asia/Tokyo"`, "JARVIS_TIMEZONE = \"UTC\"\nKEEP_ENV = \"untouched\"")
			changed += "\n[[projects]]\nname = \"other\"\n[projects.agent.options.env]\nJARVIS_API_BASE = \"http://other:1\"\n"
			if err := os.WriteFile(ccPath, []byte(changed), 0o600); err != nil {
				t.Fatal(err)
			}
			runManage(append(args, "--reuse-existing-secret")...)
			raw, err = os.ReadFile(ccPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(raw), `JARVIS_TIMEZONE = "Asia/Tokyo"`) != 1 || !strings.Contains(string(raw), `KEEP_ENV = "untouched"`) || !strings.Contains(string(raw), `JARVIS_API_BASE = "http://other:1"`) {
				t.Fatal("rebind lost unrelated env or duplicated connection")
			}
			if platform == "Linux" {
				cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "render-systemd-unit.sh"), "com.bytedance.jarvis.cc-connect", ccPath)
				cmd.Env = env
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("render systemd: %v\n%s", err, out)
				}
				unit, err := os.ReadFile(filepath.Join(root, ".config", "systemd", "user", "com.bytedance.jarvis.cc-connect.service"))
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"Environment=\"JARVIS_API_BASE=" + apiBase + "\"", "Environment=\"JARVIS_TIMEZONE=Asia/Tokyo\""} {
					if !strings.Contains(string(unit), want) {
						t.Fatalf("systemd unit missing %q", want)
					}
				}
			}
			env = append(env, "JARVIS_API_BASE=http://127.0.0.1:18843")
			runManage(append(args[:len(args)-2:len(args)-2], "--reuse-existing-secret")...)
			raw, err = os.ReadFile(ccPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, suffix := range []string{"/internal/card-approval/callback", "/internal/message-routing/claim", "/internal/meeting-sweep/wake"} {
				if !strings.Contains(string(raw), "http://127.0.0.1:18843"+suffix) {
					t.Fatalf("environment address not propagated: %s", suffix)
				}
			}

		})
	}
}

func runJarvisInstall(t *testing.T, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-install"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append(sanitizedEnv(command.Environ(), "JARVIS_API_BASE", "JARVIS_CONFIG_PATH", "JARVIS_TIMEZONE", "JARVIS_DESKTOP", "JARVIS_RESOURCE_ROOT"), extraEnv...)
	output, err := command.CombinedOutput()
	return string(output), err
}
