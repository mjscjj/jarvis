package toolcatalog

import (
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

func runJarvisInstall(t *testing.T, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-install"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append(command.Environ(), extraEnv...)
	output, err := command.CombinedOutput()
	return string(output), err
}
