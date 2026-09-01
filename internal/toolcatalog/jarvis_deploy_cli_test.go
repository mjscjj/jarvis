package toolcatalog

import (
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
		`$API_BASE/readyz`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("deploy entry missing %q", want)
		}
	}
}
