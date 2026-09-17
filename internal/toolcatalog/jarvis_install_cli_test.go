package toolcatalog

import (
	"crypto/sha1"
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
  *"run ./cmd/jarvis-config show-connection"*) printf 'configuration failed\n' >&2; exit 7 ;;
  *"run ./cmd/jarvis-config instance"*)
    printf '%s' '{"api_base":"http://127.0.0.1:19452","launchd_label":"com.bytedance.jarvis.server.test"}' ;;
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
	if decodeErr := json.Unmarshal([]byte(output), &report); decodeErr != nil {
		t.Fatalf("doctor mixed stderr into JSON output %q: %v", output, decodeErr)
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
		t.Fatalf("doctor configuration report = %#v", report.Configuration)
	}
}

func TestJarvisInstallValidateReportsBindingFailureAsJSON(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
case "$*" in
  *"run ./cmd/jarvis-config show-connection"*) printf '%s' '{"api_base":"http://127.0.0.1:19452","timezone":"Asia/Shanghai"}' ;;
  *"run ./cmd/jarvis-config instance"*) printf '%s' '{"api_base":"http://127.0.0.1:19452","launchd_label":"com.bytedance.jarvis.server.test"}' ;;
  *"run ./cmd/jarvis-config initialization-status"*) printf '%s' '{"machine_configuration_ready":true,"base_config_mode":"0600","runtime_config_mode":"0600","runtime_binaries":[]}' ;;
  *) printf '%s' "unexpected go args: $*" >&2; exit 9 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "curl"), "#!/bin/sh\nexit 1\n")
	writeExecutable(t, filepath.Join(binDir, "launchctl"), "#!/bin/sh\nexit 1\n")
	output, err := runJarvisInstall(t, []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
	}, "validate")
	if err == nil {
		t.Fatalf("validate unexpectedly passed: %s", output)
	}
	var result struct {
		OK      bool `json:"ok"`
		Binding struct {
			OK      bool            `json:"ok"`
			Details json.RawMessage `json:"details"`
			Error   string          `json:"error"`
		} `json:"integrated_bot_binding"`
	}
	if decodeErr := json.Unmarshal([]byte(output), &result); decodeErr != nil {
		t.Fatalf("validate did not return JSON %q: %v", output, decodeErr)
	}
	if result.OK || result.Binding.OK || string(result.Binding.Details) != "null" || result.Binding.Error == "" {
		t.Fatalf("validation result = %#v", result)
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
		`CC_CONNECT_PATCH_RELATIVE_PATH="integrations/cc-connect/patches/cc-connect-v1.4.1-jarvis.patch"`,
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("CC Connect manifest missing %q", want)
		}
	}
	// CC_CONNECT_PATCH_COMMIT is stamped into the binary as main.commit, so it
	// is how a running CC Connect reports which patch it was built from. Check
	// the invariant instead of pinning a literal: a regenerated patch left the
	// two out of sync once, and the stamp then named a patch nobody shipped.
	patch, err := os.ReadFile(filepath.Join(repoRoot, "integrations", "cc-connect", "patches", "cc-connect-v1.4.1-jarvis.patch"))
	if err != nil {
		t.Fatal(err)
	}
	wantStamp := fmt.Sprintf(`CC_CONNECT_PATCH_COMMIT="%x"`, sha1.Sum(patch))
	if !strings.Contains(manifest, wantStamp) {
		t.Fatalf("CC Connect manifest missing %q; the pinned stamp does not match the shipped patch", wantStamp)
	}
	builderContent, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "install-cc-connect.sh"))
	if err != nil {
		t.Fatal(err)
	}
	builder := string(builderContent)
	for _, want := range []string{
		`git -C "$source_dir" apply --check "$PATCH_PATH"`,
		`npm install --no-audit --no-fund`,
		`npm run build`,
		`go test ./platform/feishu`,
		`./agent/codex`,
		`TARGET_BIN="${REPO_ROOT}/bin/cc-connect-jarvis"`,
		`source "${REPO_ROOT}/packaging/macos/runtime-manifest.sh"`,
		`export MACOSX_DEPLOYMENT_TARGET="$JARVIS_MACOS_MIN_VERSION"`,
		`packaging/macos/check-macho.sh`,
		`go build -trimpath -tags goolm`,
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

func TestRebuildServerKeepsRegistrationOwnedByDeploy(t *testing.T) {
	scriptPath, err := filepath.Abs(filepath.Join("..", "..", "scripts", "rebuild-server.sh"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	taskGate := strings.Index(script, `running_tasks=$(running_task_count)`)
	buildServer := strings.Index(script, `go build -o "$next_bin"`)
	if taskGate < 0 || buildServer < 0 {
		t.Fatal("rebuild-server must retain its build and executing-Task guard")
	}
	if !strings.Contains(script, "instance service $label is not loaded") {
		t.Fatal("rebuild-server must direct an unregistered instance back to the deployment owner")
	}
	for _, forbidden := range []string{`"$script_dir/install-launchd.sh"`, "jarvis-install install-server", "install-cc-connect"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("rebuild-server unexpectedly takes over service registration: %q", forbidden)
		}
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
  *"run ./cmd/jarvis-config show-connection"*) printf '%s' '{"api_base":"http://127.0.0.1:19452","timezone":"Asia/Shanghai"}' ;;
  *"run ./cmd/jarvis-config instance"*)
    printf '%s' '{"api_base":"http://127.0.0.1:19452","launchd_label":"com.bytedance.jarvis.server.test"}' ;;
  *"run ./cmd/jarvis-config show-principal"*)
    printf '%s\n' 'go: downloading harmless-test-module' >&2
    printf '%s' '{"principal_open_id":"ou_ready","git_author":"ready@example.com","card_approval_enabled":true,"card_approval_principal_open_id":"ou_ready","relay_secret":"`+relaySecret+`","relay_secret_sha256":"`+relayHash+`"}' ;;
  *) printf '%s' "unexpected go args: $*" >&2; exit 9 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
if [ "$*" = "config show" ]; then
  printf '%s\n' 'Config file path: /tmp/test-config.json' >&2
  printf '%s\n' 'resolved profile:'
  printf '%s' '{"profile":"cli_ready","brand":"feishu","appId":"cli_app_ready","appSecret":"****"}'
  exit 0
fi
if [ "$*" = "auth status --json --verify" ]; then
  printf '%s\n' 'auth diagnostic' >&2
  printf '%s' '{"verified":true,"identities":{"bot":{"status":"ready","verified":true},"user":{"status":"ready","verified":true,"tokenStatus":"valid","openId":"ou_ready"}}}'
  exit 0
fi
if [ "$*" = "event consume card.action.trigger --as bot --dry-run" ]; then
  printf '%s' '{"ok":true,"identity":"bot","dry_run":true,"data":{"decision":{"event_key":"card.action.trigger","identity":"bot","status":"ready","preconditions":[{"name":"credentials_available","status":"ok"},{"name":"console_event_published","status":"ok"},{"name":"scopes_granted","status":"ok"}]}}}'
  exit 0
fi
printf '%s' "unexpected lark-cli args: $*" >&2
exit 9
`)
	writeExecutable(t, filepath.Join(binDir, "curl"), `#!/bin/sh
printf '%s' '{"code":0,"msg":"ok","tenant_access_token":"tenant-ready"}'
`)
	out, err := runJarvisInstallWithInput(t, "app-secret-ready\n", []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
	}, "bind-cc", "--cc-config", ccConfigPath)
	if err != nil {
		failedConfig, _ := os.ReadFile(ccConfigPath)
		t.Fatalf("bind-cc failed: %v\n%s\nconfig:\n%s", err, out, failedConfig)
	}
	var result struct {
		Ready  bool `json:"ready"`
		Checks struct {
			Context       bool `json:"agent_loads_jarvis_context_each_turn"`
			TrustedChatID bool `json:"cc_connect_injects_trusted_chat_id"`
			CardCallback  bool `json:"card_callback_app_permission_and_event_ready"`
			Access        bool `json:"feishu_allow_from_is_principal_only"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode bind-cc output %q: %v", out, err)
	}
	if !result.Ready || !result.Checks.Context || !result.Checks.TrustedChatID || !result.Checks.CardCallback || !result.Checks.Access {
		t.Fatalf("binding result = %#v", result)
	}
	content, err := os.ReadFile(ccConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		`name = "keep-me"`, `name = "jarvis-codex"`, `inject_sender = true`, `app_id = "cli_app_ready"`,
		`allow_from = "ou_ready"`,
		`mode = "yolo"`, `cmd = "codex"`, `scripts/jarvis-tools get-context --chat-id`, `scripts/jarvis-tools get-shared-memory`,
		`agent_identity.display_name`, `prior_messages`, `scripts/jarvis-tools create-task`, `source_type=manual`, `delivery_required`,
		`jarvis_approval_url = "http://127.0.0.1:19452/internal/card-approval/callback"`,
		`jarvis_route_claim_url = "http://127.0.0.1:19452/internal/message-routing/claim"`,
		`jarvis_event_relay_url = "http://127.0.0.1:19452/internal/meeting-sweep/wake"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("CC config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "--profile") {
		t.Fatalf("CC config must use the default lark-cli identity:\n%s", text)
	}
	writeExecutable(t, filepath.Join(binDir, "curl"), `#!/bin/sh
printf '%s' '{"code":10003,"msg":"invalid app secret"}'
`)
	invalidOutput, invalidErr := runJarvisInstallWithInput(t, "wrong-secret\n", []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
	}, "bind-cc", "--cc-config", ccConfigPath)
	if invalidErr == nil || !strings.Contains(invalidOutput, "invalid app secret") {
		t.Fatalf("bind-cc accepted invalid credentials: %v\n%s", invalidErr, invalidOutput)
	}
	unchanged, err := os.ReadFile(ccConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != text {
		t.Fatalf("invalid credentials changed CC config:\n%s", unchanged)
	}
	writeExecutable(t, filepath.Join(binDir, "curl"), `#!/bin/sh
printf '%s' '{"code":0,"msg":"ok","tenant_access_token":"tenant-ready"}'
`)

	// Existing installations may have the old global-context prompt and no
	// trusted chat coordinate. Rebinding must migrate that same project in
	// place instead of requiring users to delete or duplicate it.
	legacyText := strings.Replace(text, "inject_sender = true", "inject_sender = false", 1)
	legacyText = strings.Replace(legacyText, "get-context --chat-id", "get-context", 1)
	legacyText = strings.Replace(legacyText, `allow_from = "ou_ready"`, `allow_from = "*"`, 1)
	legacyText = strings.Replace(legacyText, `cmd = "codex"`, `cmd = "traex"`, 1)
	for _, legacyRelayLine := range []string{
		`jarvis_event_relay_url = "http://127.0.0.1:18800/internal/meeting-sweep/wake"` + "\n",
		`jarvis_event_relay_secret = "` + relaySecret + `"` + "\n",
		`jarvis_event_relay_types = "vc.meeting.participant_meeting_ended_v1"` + "\n",
	} {
		legacyText = strings.Replace(legacyText, legacyRelayLine, "", 1)
	}
	if err := os.WriteFile(ccConfigPath, []byte(legacyText), 0o600); err != nil {
		t.Fatal(err)
	}
	refusedOutput, refusedErr := runJarvisInstallWithInput(t, "app-secret-ready\n", []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
	}, "bind-cc", "--cc-config", ccConfigPath)
	if refusedErr == nil || !strings.Contains(refusedOutput, "--replace-allow-from") {
		t.Fatalf("bind-cc did not require explicit allow_from replacement: %v\n%s", refusedErr, refusedOutput)
	}
	if _, err := runJarvisInstallWithInput(t, "app-secret-ready\n", []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
	}, "bind-cc", "--replace-allow-from", "--cc-config", ccConfigPath); err != nil {
		t.Fatalf("rebind legacy CC config: %v", err)
	}
	migrated, err := os.ReadFile(ccConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	migratedText := string(migrated)
	if strings.Count(migratedText, "inject_sender = true") != 1 || strings.Contains(migratedText, "inject_sender = false") {
		t.Fatalf("legacy CC config did not migrate inject_sender exactly once:\n%s", migratedText)
	}
	if !strings.Contains(migratedText, "scripts/jarvis-tools get-context --chat-id") {
		t.Fatalf("legacy CC config did not migrate to chat-scoped context:\n%s", migratedText)
	}
	if !strings.Contains(migratedText, "scripts/jarvis-tools get-shared-memory") {
		t.Fatalf("legacy CC config did not migrate shared memory injection:\n%s", migratedText)
	}
	if !strings.Contains(migratedText, `cmd = "traex"`) {
		t.Fatalf("rebind did not preserve the compatible TraeX CLI:\n%s", migratedText)
	}
	if strings.Count(migratedText, `allow_from = "ou_ready"`) != 1 || strings.Contains(migratedText, `allow_from = "*"`) {
		t.Fatalf("legacy CC config did not migrate allow_from to principal-only exactly once:\n%s", migratedText)
	}
	for _, want := range []string{
		`jarvis_event_relay_url = "http://127.0.0.1:19452/internal/meeting-sweep/wake"`,
		`jarvis_event_relay_secret = "` + relaySecret + `"`,
		`jarvis_event_relay_types = "vc.meeting.participant_meeting_ended_v1"`,
	} {
		if strings.Count(migratedText, want) != 1 {
			t.Fatalf("legacy CC config did not restore %q exactly once:\n%s", want, migratedText)
		}
	}

	missingAllowFromText := strings.Replace(migratedText, "allow_from = \"ou_ready\"\n", "", 1)
	if err := os.WriteFile(ccConfigPath, []byte(missingAllowFromText), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runJarvisInstallWithInput(t, "app-secret-ready\n", []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
	}, "bind-cc", "--cc-config", ccConfigPath); err != nil {
		t.Fatalf("rebind CC config without allow_from: %v", err)
	}
	completed, err := os.ReadFile(ccConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	completedText := string(completed)
	if strings.Count(completedText, `allow_from = "ou_ready"`) != 1 {
		t.Fatalf("CC config without allow_from was not completed with principal-only access exactly once:\n%s", completedText)
	}
}

func TestJarvisInstallReusesReadyLarkCLIAndTraex(t *testing.T) {
	binDir := t.TempDir()
	homeDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'lark-cli version 1.0.93'
  exit 0
fi
if [ "$*" = "event consume card.action.trigger --help" ]; then
  printf '%s\n' 'usage: lark-cli event consume [--dry-run]'
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
	for _, skillName := range []string{"lark-shared", "lark-contact", "lark-drive", "lark-doc", "lark-im"} {
		skillPath := filepath.Join(homeDir, ".agents", "skills", skillName)
		if err := os.MkdirAll(skillPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
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

func TestJarvisInstallUpdatesExistingLarkCLIAndSkillsTogether(t *testing.T) {
	binDir := t.TempDir()
	homeDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "updated")
	writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
if [ "$1" = "update" ]; then
  touch "$UPDATE_MARKER"
  for skill in lark-shared lark-contact lark-drive lark-doc lark-im; do
    mkdir -p "$HOME/.agents/skills/$skill"
    printf '%s\n' test >"$HOME/.agents/skills/$skill/SKILL.md"
  done
  exit 0
fi
if [ "$*" = "event consume card.action.trigger --help" ]; then
  if [ -f "$UPDATE_MARKER" ]; then
    printf '%s\n' 'usage: lark-cli event consume [--dry-run]'
  else
    printf '%s\n' 'usage: old lark-cli event consume'
  fi
  exit 0
fi
if [ "$1" = "--version" ]; then
  if [ -f "$UPDATE_MARKER" ]; then
    printf '%s\n' 'lark-cli version 1.0.93'
  else
    printf '%s\n' 'lark-cli version 1.0.80'
  fi
  exit 0
fi
exit 9
`)
	writeExecutable(t, filepath.Join(binDir, "npx"), "#!/bin/sh\nprintf '%s\\n' 'unexpected npm installer invocation' >&2\nexit 99\n")
	output, err := runJarvisInstall(t, []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + homeDir,
		"CODEX_HOME=",
		"UPDATE_MARKER=" + marker,
	}, "install-lark-cli")
	if err != nil {
		t.Fatalf("install-lark-cli update: %v: %s", err, output)
	}
	var result struct {
		Changed       bool `json:"changed"`
		ProtocolReady bool `json:"required_protocol_ready"`
		SkillPack     bool `json:"agent_skill_pack_detected"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.ProtocolReady || !result.SkillPack {
		t.Fatalf("lark update result = %#v", result)
	}
}

func TestJarvisInstallUpdatesOldLarkCLIEvenWhenSkillsAndProtocolExist(t *testing.T) {
	binDir := t.TempDir()
	homeDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "updated")
	for _, skillName := range []string{"lark-shared", "lark-contact", "lark-drive", "lark-doc", "lark-im"} {
		skillPath := filepath.Join(homeDir, ".agents", "skills", skillName)
		if err := os.MkdirAll(skillPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
if [ "$*" = "event consume card.action.trigger --help" ]; then
  printf '%s\n' 'usage: lark-cli event consume [--dry-run]'
  exit 0
fi
if [ "$1" = "--version" ]; then
  if [ -f "$UPDATE_MARKER" ]; then
    printf '%s\n' 'lark-cli version 1.0.93'
  else
    printf '%s\n' 'lark-cli version 1.0.80'
  fi
  exit 0
fi
if [ "$1" = "update" ] && [ "$2" = "--json" ]; then
  touch "$UPDATE_MARKER"
  exit 0
fi
exit 9
`)
	output, err := runJarvisInstall(t, []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + homeDir,
		"CODEX_HOME=",
		"UPDATE_MARKER=" + marker,
	}, "install-lark-cli")
	if err != nil {
		t.Fatalf("install-lark-cli old version: %v: %s", err, output)
	}
	var result struct {
		Changed bool   `json:"changed"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Version != "lark-cli version 1.0.93" {
		t.Fatalf("old lark-cli update result = %#v", result)
	}
}

func TestJarvisInstallAcceptsOfficialLarkSuiteLayout(t *testing.T) {
	binDir := t.TempDir()
	homeDir := t.TempDir()
	suitePath := filepath.Join(homeDir, ".agents", "skills", "lark-suite")
	if err := os.MkdirAll(suitePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(suitePath, "SKILL.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "lark-cli"), `#!/bin/sh
if [ "$*" = "event consume card.action.trigger --help" ]; then
  printf '%s\n' 'usage: lark-cli event consume [--dry-run]'
  exit 0
fi
if [ "$1" = "--version" ]; then
  printf '%s\n' 'lark-cli version 1.0.93'
  exit 0
fi
if [ "$1" = "update" ]; then
  printf '%s\n' 'unexpected update' >&2
  exit 99
fi
exit 9
`)
	output, err := runJarvisInstall(t, []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + homeDir,
		"CODEX_HOME=",
	}, "install-lark-cli")
	if err != nil {
		t.Fatalf("install-lark-cli suite layout: %v: %s", err, output)
	}
	var result struct {
		Changed   bool   `json:"changed"`
		SkillPack bool   `json:"agent_skill_pack_detected"`
		SkillPath string `json:"agent_skill_pack_path"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Changed || !result.SkillPack || result.SkillPath != filepath.Join(suitePath, "SKILL.md") {
		t.Fatalf("suite layout result = %#v", result)
	}
}

func TestJarvisInstallInstallsOfficialCodexWhenMissing(t *testing.T) {
	binDir := t.TempDir()
	for _, name := range []string{"bash", "cat", "chmod", "dirname", "head", "uname"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(binDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(jqPath, filepath.Join(binDir, "jq")); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "npm"), `#!/bin/sh
if [ "$*" != "install --global @openai/codex@latest" ]; then
  printf '%s\n' "unexpected npm args: $*" >&2
  exit 9
fi
cat >"$TEST_BIN/codex" <<'SCRIPT'
#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'codex-cli test'
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  printf '%s\n' 'Logged in using test credentials'
  exit 0
fi
exit 9
SCRIPT
chmod 700 "$TEST_BIN/codex"
`)
	output, err := runJarvisInstall(t, []string{
		"PATH=" + binDir,
		"TEST_BIN=" + binDir,
	}, "install-codex")
	if err != nil {
		t.Fatalf("install-codex: %v: %s", err, output)
	}
	var result struct {
		Changed    bool   `json:"changed"`
		Path       string `json:"path"`
		Version    string `json:"version"`
		LoginReady bool   `json:"login_ready"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Path != filepath.Join(binDir, "codex") || result.Version != "codex-cli test" || !result.LoginReady {
		t.Fatalf("codex install result = %#v", result)
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
if [ "$*" = "event consume card.action.trigger --help" ]; then
  printf '%s\n' 'usage: lark-cli event consume [--dry-run]'
  exit 0
fi
printf '%s\n' 'lark-cli version 1.0.93'
SCRIPT
chmod 700 "$TEST_BIN/lark-cli"
for skill in lark-shared lark-contact lark-drive lark-doc lark-im; do
  mkdir -p "$HOME/.agents/skills/$skill"
  printf '%s\n' test >"$HOME/.agents/skills/$skill/SKILL.md"
done
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

func TestJarvisInstallExposesBytedCLIToLaunchd(t *testing.T) {
	binDir := t.TempDir()
	homeDir := t.TempDir()
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(jqPath, filepath.Join(binDir, "jq")); err != nil {
		t.Fatal(err)
	}
	bytedCLIPath := filepath.Join(binDir, "bytedcli")
	writeExecutable(t, bytedCLIPath, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' '0.137.0'
  exit 0
fi
if [ "$*" = "--json auth status" ]; then
  printf '%s\n' '{"data":{"authenticated":true}}'
  exit 0
fi
exit 9
`)
	writeExecutable(t, filepath.Join(binDir, "npm"), "#!/bin/sh\necho unexpected npm invocation >&2\nexit 99\n")

	output, err := runJarvisInstall(t, []string{
		"PATH=" + binDir + ":/usr/bin:/bin",
		"HOME=" + homeDir,
	}, "install-bytedcli")
	if err != nil {
		t.Fatalf("install-bytedcli: %v: %s", err, output)
	}
	var result struct {
		Path       string `json:"path"`
		Version    string `json:"version"`
		LoginReady bool   `json:"login_ready"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	physicalHome, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	stablePath := filepath.Join(physicalHome, ".local", "bin", "bytedcli")
	if result.Path != stablePath || result.Version != "0.137.0" || !result.LoginReady {
		t.Fatalf("bytedcli install result = %#v", result)
	}
	target, err := os.Readlink(stablePath)
	if err != nil {
		t.Fatal(err)
	}
	if target != bytedCLIPath {
		t.Fatalf("stable bytedcli symlink = %q, want %q", target, bytedCLIPath)
	}
}

func TestJarvisInstallRefusesToReplaceLoadedServer(t *testing.T) {
	binDir := t.TempDir()
	for _, commandName := range []string{"curl", "npm", "lark-cli", "traex", "plutil"} {
		writeExecutable(t, filepath.Join(binDir, commandName), "#!/bin/sh\nexit 0\n")
	}
	writeExecutable(t, filepath.Join(binDir, "uname"), "#!/bin/sh\nprintf '%s\\n' Darwin\n")
	writeExecutable(t, filepath.Join(binDir, "go"), "#!/bin/sh\nprintf '%s\\n' '{\"api_base\":\"http://127.0.0.1:19452\",\"launchd_label\":\"com.bytedance.jarvis.server.test\"}'\n")
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
	writeExecutable(t, filepath.Join(binDir, "systemctl"), "#!/bin/sh\nexit 1\n")
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
case "$*" in
  *"run ./cmd/jarvis-config show-connection"*) printf '%s' '{"api_base":"http://127.0.0.1:19452","timezone":"Asia/Shanghai"}' ;;
  *"run ./cmd/jarvis-config instance"*) printf '%s' '{"api_base":"http://127.0.0.1:19452","launchd_label":"com.bytedance.jarvis.server.test"}' ;;
  *"run ./cmd/jarvis-config initialization-status"*) printf '%s' '{"machine_configuration_ready":true,"runtime_binaries":["traex"],"runtime_config_path":"/unused/config.runtime.yaml"}' ;;
  *) printf '%s' "unexpected go args: $*" >&2; exit 9 ;;
esac
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
	return runJarvisInstallWithInput(t, "", extraEnv, args...)
}

func runJarvisInstallWithInput(t *testing.T, input string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "jarvis-install"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append(sanitizedEnv(command.Environ(), "JARVIS_API_BASE", "JARVIS_CONFIG", "JARVIS_CONFIG_PATH", "JARVIS_TIMEZONE", "JARVIS_DESKTOP", "JARVIS_RESOURCE_ROOT"), extraEnv...)
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	return string(output), err
}
