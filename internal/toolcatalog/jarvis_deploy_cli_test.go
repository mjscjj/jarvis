package toolcatalog

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJarvisDeployIsOneCrossPlatformEntry(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repoRoot, "scripts", "jarvis-deploy")
	command := exec.Command("bash", script, "--help")
	help, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--config", "--remote-okr-db", "--skip-pull", "Darwin", "Linux", "launchd", "systemd"} {
		if !strings.Contains(string(help), want) {
			t.Fatalf("deploy help missing %q:\n%s", want, help)
		}
	}
	content, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		`git pull --ff-only`,
		`"${SCRIPT_DIR}/rebuild-server.sh"`,
		`"${SCRIPT_DIR}/jarvis-install" install-server`,
		`systemctl --user restart`,
		`go build -o "$main_next" ./cmd/jarvis-server`,
		`service_path="${HOME}/.local/bin:`,
		`$API_BASE/readyz`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("deploy entry missing %q", want)
		}
	}
}

func TestRetireChatSidecarStopsBeforeDeleting(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"Linux", "Darwin"} {
		for _, failStop := range []bool{false, true} {
			t.Run(platform+"/stop-fails="+fmt.Sprint(failStop), func(t *testing.T) {
				// Intercept service commands and rm: never touch the developer's
				// service manager, HOME, binaries, or logs in these tests.
				command := exec.Command("bash", "-c", `
set -euo pipefail
source "$1/scripts/lib/retire-chat-sidecar.sh"
uname() { printf '%s\n' "$REVIEW_PLATFORM"; }
systemctl() {
  printf 'systemctl %s\n' "$*"
  if [[ "$*" == *'disable --now'* && "$FAIL_STOP" == true ]]; then return 1; fi
}
launchctl() {
  printf 'launchctl %s\n' "$*"
  if [[ "$1" == bootout && "$FAIL_STOP" == true ]]; then return 1; fi
}
rm() { printf 'REMOVE %s\n' "$*"; }
retire_chat_sidecar 'review-instance' '/review-repo'
`, "review", root)
				command.Env = append(os.Environ(), "REVIEW_PLATFORM="+platform, "FAIL_STOP="+fmt.Sprint(failStop))
				output, err := command.CombinedOutput()
				if failStop {
					if err == nil || strings.Contains(string(output), "REMOVE") {
						t.Fatalf("deleted files after stop failure: %s, %v", output, err)
					}
					return
				}
				if err != nil || !strings.Contains(string(output), "review-instance.chat") || !strings.Contains(string(output), "REMOVE") {
					t.Fatalf("cleanup failed: %s, %v", output, err)
				}
			})
		}
	}
}
