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

func TestJarvisInstallIsProjectOwnedAndAgentDriven(t *testing.T) {
	help, err := runJarvisInstall(t, nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"start",
		"doctor",
		"install-lark-cli",
		"install-traex",
		"install-cc-connect",
		"install-qdrant",
		"validate-dependencies",
		"configure-identity",
		"bind-cc",
		"validate-binding",
		"install-server",
		"validate",
		"status",
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

func TestJarvisInstallCreatesOneAuditableProjectChecklist(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(repoRoot, "var", "install", fmt.Sprintf("test-%d", time.Now().UnixNano()))
	defer os.RemoveAll(runDir)
	out, err := runJarvisInstall(t, nil, "start", "--profile", "cli_ready", "--run-dir", runDir)
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
	content, err := os.ReadFile(created.Checklist)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		"## A. 仓库与安装决策",
		"## B. 工具链与全部依赖",
		"## C. 飞书身份与一体化绑定",
		"## D. 服务启动与运行底座验收",
		"## E. 世界模型建立",
		"## F. 真实端到端验收",
		"## 未完成、未做或不适用",
		"cli_ready",
		"- [ ]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("checklist missing %q:\n%s", want, text)
		}
	}
	status, err := runJarvisInstall(t, nil, "status", "--run-dir", runDir)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Completed int  `json:"completed"`
		Pending   int  `json:"pending"`
		Complete  bool `json:"complete"`
	}
	if err := json.Unmarshal([]byte(status), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Completed != 0 || summary.Pending == 0 || summary.Complete {
		t.Fatalf("status = %#v", summary)
	}
}

func TestJarvisInstallPinsPatchedCCConnectWithoutStartingIt(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	manifestContent, err := os.ReadFile(filepath.Join(repoRoot, "integrations", "cc-connect", "manifest.sh"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(manifestContent)
	for _, want := range []string{
		`CC_CONNECT_BASE_COMMIT="5d4c96dd12774574369e75b60084140101c9a59a"`,
		`CC_CONNECT_PATCH_COMMIT="ab5871904ecba1e467726f2b1a184349eb45d06c"`,
		`CC_CONNECT_PATCH_RELATIVE_PATH="integrations/cc-connect/patches/cc-connect-v1.4.1-jarvis.patch"`,
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("CC Connect manifest missing %q", want)
		}
	}
	builderContent, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "install-cc-connect.sh"))
	if err != nil {
		t.Fatal(err)
	}
	builder := string(builderContent)
	for _, want := range []string{
		`git -C "$source_dir" apply --check "$PATCH_PATH"`,
		`go test ./platform/feishu`,
		`TARGET_BIN="${REPO_ROOT}/bin/cc-connect-jarvis"`,
	} {
		if !strings.Contains(builder, want) {
			t.Fatalf("CC Connect build implementation missing %q", want)
		}
	}
	for _, forbidden := range []string{"daemon install", "daemon start", "config.toml"} {
		if strings.Contains(builder, forbidden) {
			t.Fatalf("install-cc-connect must not configure or start a daemon; found %q", forbidden)
		}
	}
	patchPath := filepath.Join(repoRoot, "integrations", "cc-connect", "patches", "cc-connect-v1.4.1-jarvis.patch")
	patchInfo, err := os.Stat(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if patchInfo.Size() < 50_000 {
		t.Fatalf("vendored CC Connect patch is unexpectedly small: %d bytes", patchInfo.Size())
	}
}

func TestJarvisInstallGatesServerStartOnDependencyValidation(t *testing.T) {
	scriptPath, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-install"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	gate := strings.Index(script, `dependency_validation="$(validate_dependencies)"`)
	start := strings.Index(script, `"${REPO_ROOT}/scripts/install-launchd.sh"`)
	if gate < 0 || start < 0 || gate >= start {
		t.Fatalf("install-server must validate all dependencies before starting Jarvis")
	}
}

func TestJarvisInstallConfiguresIdentityThroughMachineBoundary(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
case "$*" in
  *"run ./cmd/jarvis-config configure-principal"*"--open-id ou_ready --profile cli_ready --git-author ready@example.com")
    printf '%s' '{"principal_open_id":"ou_ready","lark_profile":"cli_ready","git_author":"ready@example.com"}' ;;
  *) printf '%s' "unexpected go args: $*" >&2; exit 9 ;;
esac
`)
	out, err := runJarvisInstall(t, []string{"PATH=" + binDir + ":" + os.Getenv("PATH")},
		"configure-identity", "--open-id", "ou_ready", "--profile", "cli_ready", "--git-author", "ready@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"principal_open_id":"ou_ready"`) {
		t.Fatalf("configure-identity output = %s", out)
	}
}

func TestJarvisInstallBindsCCAndBootstrapsJarvisContext(t *testing.T) {
	binDir := t.TempDir()
	ccConfigPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(ccConfigPath, []byte("[[projects]]\nname = \"keep-me\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	relaySecret := "relay-ready"
	relayHash := fmt.Sprintf("%x", sha256.Sum256([]byte(relaySecret)))
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
case "$*" in
  *"run ./cmd/jarvis-config show-principal"*)
    printf '%s' '{"principal_open_id":"ou_ready","lark_profile":"cli_ready","git_author":"ready@example.com","card_approval_enabled":true,"card_approval_profile":"cli_ready","card_approval_principal_open_id":"ou_ready","relay_secret":"`+relaySecret+`","relay_secret_sha256":"`+relayHash+`"}' ;;
  *) printf '%s' "unexpected go args: $*" >&2; exit 9 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
if [ "$1" = "config" ] && [ "$2" = "show" ]; then
  printf '%s\n' 'resolved profile:'
  printf '%s' '{"profile":"cli_ready","appId":"cli_app_ready","appSecret":"****"}'
  exit 0
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  printf '%s' '{"verified":true,"identities":{"bot":{"status":"ready","verified":true},"user":{"status":"ready","verified":true,"tokenStatus":"valid","openId":"ou_ready"}}}'
  exit 0
fi
printf '%s' "unexpected lark-cli args: $*" >&2
exit 9
`)
	out, err := runJarvisInstallWithInput(t, "app-secret-ready\n", []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
	}, "bind-cc", "--profile", "cli_ready", "--cc-config", ccConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Ready  bool `json:"ready"`
		Checks struct {
			Context bool `json:"agent_loads_jarvis_context_each_turn"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Ready || !result.Checks.Context {
		t.Fatalf("binding result = %#v", result)
	}
	content, err := os.ReadFile(ccConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{`name = "keep-me"`, `name = "jarvis-codex"`, `app_id = "cli_app_ready"`, `scripts/jarvis-tools get-context`, `--profile cli_ready`} {
		if !strings.Contains(text, want) {
			t.Fatalf("CC config missing %q:\n%s", want, text)
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
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-install"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append(command.Environ(), extraEnv...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func runJarvisInstallWithInput(t *testing.T, input string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-install"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append(command.Environ(), extraEnv...)
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	return string(output), err
}
