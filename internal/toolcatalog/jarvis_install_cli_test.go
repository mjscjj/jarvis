package toolcatalog

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJarvisInstallIsSkillLocalAndAgentDriven(t *testing.T) {
	help, err := runJarvisInstall(t, nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"doctor",
		"install-lark-cli",
		"install-traex",
		"install-qdrant",
		"install-server",
		"validate",
		"The calling",
		"Agent decides",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("jarvis-install help missing %q:\n%s", want, help)
		}
	}

	toolsHelp, err := runJarvisTools(t, "", nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"install-qdrant", "install-server", "initialization-status"} {
		if strings.Contains(toolsHelp, forbidden) {
			t.Fatalf("jarvis-tools exposes installation-only command %q:\n%s", forbidden, toolsHelp)
		}
	}
}

func TestJarvisInstallReusesReadyLarkCLIAndTraex(t *testing.T) {
	binDir := t.TempDir()
	homeDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'lark-cli version test'
  exit 0
fi
exit 9
`)
	writeExecutable(t, filepath.Join(binDir, "traex"), `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'traecli test (internal edition)'
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  printf '%s\n' 'Logged in using test SSO'
  exit 0
fi
exit 9
`)
	for _, commandName := range []string{"npx", "curl"} {
		writeExecutable(t, filepath.Join(binDir, commandName), "#!/bin/sh\necho unexpected installer invocation >&2\nexit 99\n")
	}
	writeExecutable(t, filepath.Join(binDir, "uname"), `#!/bin/sh
case "$1" in
  -s) printf '%s\n' Darwin ;;
  -m) printf '%s\n' arm64 ;;
  *) exit 9 ;;
esac
`)
	skillPath := filepath.Join(homeDir, ".agents", "skills", "lark-shared")
	if err := os.MkdirAll(skillPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + homeDir,
		"CODEX_HOME=",
	}

	larkOutput, err := runJarvisInstall(t, env, "install-lark-cli")
	if err != nil {
		t.Fatalf("install-lark-cli: %v: %s", err, larkOutput)
	}
	var larkResult struct {
		Changed   bool `json:"changed"`
		SkillPack bool `json:"agent_skill_pack_detected"`
	}
	if err := json.Unmarshal([]byte(larkOutput), &larkResult); err != nil {
		t.Fatal(err)
	}
	if larkResult.Changed || !larkResult.SkillPack {
		t.Fatalf("lark install result = %#v", larkResult)
	}

	traexOutput, err := runJarvisInstall(t, env, "install-traex")
	if err != nil {
		t.Fatalf("install-traex: %v: %s", err, traexOutput)
	}
	var traexResult struct {
		Changed    bool `json:"changed"`
		LoginReady bool `json:"login_ready"`
	}
	if err := json.Unmarshal([]byte(traexOutput), &traexResult); err != nil {
		t.Fatal(err)
	}
	if traexResult.Changed || !traexResult.LoginReady {
		t.Fatalf("traex install result = %#v", traexResult)
	}
}

func TestJarvisInstallRunsOfficialInstallersWhenCLIsAreMissing(t *testing.T) {
	binDir := t.TempDir()
	homeDir := t.TempDir()
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(jqPath, filepath.Join(binDir, "jq")); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "npx"), `#!/bin/sh
if [ "$*" != "--yes @larksuite/cli@latest install" ]; then
  printf '%s\n' "unexpected npx args: $*" >&2
  exit 9
fi
cat >"$TEST_BIN/lark-cli" <<'SCRIPT'
#!/bin/sh
printf '%s\n' 'lark-cli version installed-test'
SCRIPT
chmod 700 "$TEST_BIN/lark-cli"
mkdir -p "$HOME/.agents/skills/lark-shared"
printf '%s\n' test >"$HOME/.agents/skills/lark-shared/SKILL.md"
`)
	writeExecutable(t, filepath.Join(binDir, "curl"), `#!/bin/sh
target=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    target="$2"
    shift 2
    continue
  fi
  shift
done
if [ -z "$target" ]; then
  printf '%s\n' 'missing curl output target' >&2
  exit 9
fi
cat >"$target" <<'INSTALLER'
#!/bin/sh
mkdir -p "$HOME/.local/bin"
cat >"$HOME/.local/bin/traex" <<'TRAE'
#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'traecli installed-test (internal edition)'
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  printf '%s\n' 'Not logged in'
  exit 1
fi
exit 9
TRAE
chmod 700 "$HOME/.local/bin/traex"
INSTALLER
`)
	writeExecutable(t, filepath.Join(binDir, "uname"), `#!/bin/sh
case "$1" in
  -s) printf '%s\n' Darwin ;;
  -m) printf '%s\n' arm64 ;;
  *) exit 9 ;;
esac
`)
	env := []string{
		"PATH=" + binDir + ":/usr/bin:/bin",
		"HOME=" + homeDir,
		"CODEX_HOME=",
		"TEST_BIN=" + binDir,
	}

	larkOutput, err := runJarvisInstall(t, env, "install-lark-cli")
	if err != nil {
		t.Fatalf("install-lark-cli: %v: %s", err, larkOutput)
	}
	var larkResult struct {
		Changed   bool `json:"changed"`
		SkillPack bool `json:"agent_skill_pack_detected"`
	}
	if err := json.Unmarshal([]byte(larkOutput), &larkResult); err != nil {
		t.Fatal(err)
	}
	if !larkResult.Changed || !larkResult.SkillPack {
		t.Fatalf("lark install result = %#v", larkResult)
	}

	traexOutput, err := runJarvisInstall(t, env, "install-traex")
	if err != nil {
		t.Fatalf("install-traex: %v: %s", err, traexOutput)
	}
	var traexResult struct {
		Changed              bool `json:"changed"`
		LoginReady           bool `json:"login_ready"`
		ShellRefreshRequired bool `json:"shell_refresh_required"`
	}
	if err := json.Unmarshal([]byte(traexOutput), &traexResult); err != nil {
		t.Fatal(err)
	}
	if !traexResult.Changed || traexResult.LoginReady || !traexResult.ShellRefreshRequired {
		t.Fatalf("traex install result = %#v", traexResult)
	}
}

func TestJarvisInstallRefusesToReplaceLoadedServer(t *testing.T) {
	binDir := t.TempDir()
	for _, commandName := range []string{"curl", "go", "npm", "lark-cli", "traex"} {
		writeExecutable(t, filepath.Join(binDir, commandName), "#!/bin/sh\nexit 0\n")
	}
	writeExecutable(t, filepath.Join(binDir, "launchctl"), "#!/bin/sh\nexit 0\n")

	output, err := runJarvisInstall(t, []string{"PATH=" + binDir + ":" + os.Getenv("PATH")}, "install-server")
	if err == nil {
		t.Fatalf("install-server unexpectedly replaced a loaded service: %s", output)
	}
	if !strings.Contains(output, "already loaded") {
		t.Fatalf("install-server error does not explain the ownership stop:\n%s", output)
	}
}

func TestJarvisInstallRefusesServerInstallWhenTraexIsNotLoggedIn(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "launchctl"), "#!/bin/sh\nexit 1\n")
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
printf '%s\n' '{"machine_configuration_ready":true,"runtime_binaries":["traex"],"runtime_config_path":"/unused/config.runtime.yaml"}'
`)
	writeExecutable(t, filepath.Join(binDir, "traex"), `#!/bin/sh
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  printf '%s\n' 'Not logged in'
  exit 1
fi
printf '%s\n' 'traecli test (internal edition)'
`)
	for _, commandName := range []string{"curl", "npm"} {
		writeExecutable(t, filepath.Join(binDir, commandName), "#!/bin/sh\nexit 0\n")
	}

	output, err := runJarvisInstall(t, []string{"PATH=" + binDir + ":" + os.Getenv("PATH")}, "install-server")
	if err == nil {
		t.Fatalf("install-server unexpectedly accepted unauthenticated traex: %s", output)
	}
	if !strings.Contains(output, "traex is configured but not logged in") {
		t.Fatalf("install-server error does not expose missing traex login:\n%s", output)
	}
}

func runJarvisInstall(t *testing.T, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", ".agents", "skills", "install-jarvis", "scripts", "jarvis-install"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append(command.Environ(), extraEnv...)
	output, err := command.CombinedOutput()
	return string(output), err
}
