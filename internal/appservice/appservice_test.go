package appservice

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"jarvis/internal/config"
)

func TestResolveOptionsUsesExplicitRoots(t *testing.T) {
	resourceRoot := filepath.Join(t.TempDir(), "resources")
	stateRoot := filepath.Join(t.TempDir(), "state")
	options, err := ResolveOptions(Options{
		ResourceRoot: resourceRoot,
		StateRoot:    stateRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.ResourceRoot != resourceRoot || options.StateRoot != stateRoot {
		t.Fatalf("resolved options = %#v", options)
	}
	if options.Address != defaultAddress || options.StartupTimeout != defaultStartupTimeout {
		t.Fatalf("resolved defaults = %#v", options)
	}
	layout := NewLayout(options)
	if layout.ConfigPath != filepath.Join(stateRoot, "runtime", "conf", "config.yaml") {
		t.Fatalf("config path = %q", layout.ConfigPath)
	}
	if got := layout.Connection(options.Address); got.HTTPURL != "http://127.0.0.1:18800" || got.DataRoot != stateRoot {
		t.Fatalf("runtime connection = %#v", got)
	}
}

func TestResolveOptionsRejectsNonLoopbackAddress(t *testing.T) {
	_, err := ResolveOptions(Options{
		ResourceRoot: t.TempDir(),
		StateRoot:    t.TempDir(),
		Address:      "0.0.0.0:18800",
	})
	if err == nil || !strings.Contains(err.Error(), "must use loopback") {
		t.Fatalf("ResolveOptions() error = %v", err)
	}
}

func TestPrepareSyncsBundleAndPreservesUserChanges(t *testing.T) {
	resourceRoot := t.TempDir()
	stateRoot := t.TempDir()
	writeBundleFixture(t, resourceRoot)
	layout := NewLayout(Options{ResourceRoot: resourceRoot, StateRoot: stateRoot})
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(layout.RuntimeRoot, "conf", "config.yaml")
	webPath := filepath.Join(layout.RuntimeRoot, "web", "dist", "index.html")
	promptPath := filepath.Join(layout.RuntimeRoot, "conf", "prompts", "m3.md")
	if got := readFile(t, configPath); got != "version: 1\n" {
		t.Fatalf("copied config = %q", got)
	}
	if got := readFile(t, webPath); got != "web-v1\n" {
		t.Fatalf("copied web = %q", got)
	}
	if got := readFile(t, filepath.Join(layout.RuntimeRoot, "data", "shared-memory.md")); got != "" {
		t.Fatalf("initial shared memory = %q, want empty", got)
	}
	if err := os.WriteFile(promptPath, []byte("user edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceRoot, "web", "dist", "index.html"), []byte("web-v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceRoot, "conf", "prompts", "m3.md"), []byte("prompt-v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, webPath); got != "web-v2\n" {
		t.Fatalf("unmodified asset was not upgraded: %q", got)
	}
	if got := readFile(t, promptPath); got != "user edit\n" {
		t.Fatalf("user-edited asset was overwritten: %q", got)
	}
}

func TestPrepareUpgradesModifiedProgramsAndRemovesObsoletePrograms(t *testing.T) {
	resources := t.TempDir()
	writeBundleFixture(t, resources)
	layout := NewLayout(Options{ResourceRoot: resources, StateRoot: t.TempDir()})
	obsolete := "scripts/obsolete-helper.sh"
	if err := os.WriteFile(filepath.Join(resources, obsolete), []byte("old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"scripts/jarvis-tools", "scripts/lib/jarvis-tools/world.sh", "scripts/json-api-data.mjs", "web/dist/index.html", obsolete} {
		if err := os.WriteFile(filepath.Join(layout.RuntimeRoot, path), []byte("local edit\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(resources, obsolete)); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"scripts/jarvis-tools", "scripts/lib/jarvis-tools/world.sh", "scripts/json-api-data.mjs", "web/dist/index.html"} {
		if readFile(t, filepath.Join(layout.RuntimeRoot, path)) != readFile(t, filepath.Join(resources, path)) {
			t.Errorf("program asset not synchronized: %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(layout.RuntimeRoot, obsolete)); !os.IsNotExist(err) {
		t.Fatalf("obsolete program still present: %v", err)
	}
}

func TestPrepareRejectsIncompleteToolModules(t *testing.T) {
	resources := t.TempDir()
	writeBundleFixture(t, resources)
	if err := os.Remove(filepath.Join(resources, "scripts/lib/jarvis-tools/evidence.sh")); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(NewLayout(Options{ResourceRoot: resources, StateRoot: t.TempDir()})); err == nil {
		t.Fatal("accepted bundle missing evidence tools")
	}
}

func TestPrepareCreatesBootstrapOverrideOnce(t *testing.T) {
	resourceRoot := t.TempDir()
	stateRoot := t.TempDir()
	writeBundleFixture(t, resourceRoot)
	layout := NewLayout(Options{ResourceRoot: resourceRoot, StateRoot: stateRoot})
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(layout.RuntimeRoot, "conf", "config.runtime.yaml")
	override := readFile(t, overridePath)
	for _, want := range []string{
		"extract:\n  enabled: false",
		`principal_open_id: "ou_desktop_onboarding_pending"`,
		"execute:\n  enabled: false",
		"factengine:\n  enabled: false",
		`git_author: "__desktop_onboarding_pending__"`,
	} {
		if !strings.Contains(override, want) {
			t.Fatalf("bootstrap override missing %q:\n%s", want, override)
		}
	}
	if err := os.WriteFile(overridePath, []byte("identity:\n  display_name: Friday\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, overridePath); got != "identity:\n  display_name: Friday\n" {
		t.Fatalf("existing runtime override was replaced: %q", got)
	}
}

func TestAcquireProcessLockRejectsLiveOwnerAndRecoversStaleOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.pid")
	release, err := acquireProcessLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := acquireProcessLock(path); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second lock error = %v", err)
	}
	release()
	if err := os.WriteFile(path, []byte("99999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered, err := acquireProcessLock(path)
	if err != nil {
		t.Fatalf("recover stale lock: %v", err)
	}
	recovered()
}

func TestMonitorParentDisabledDoesNotClose(t *testing.T) {
	ctx, cancel := contextWithTimeout(t, 20*time.Millisecond)
	defer cancel()
	select {
	case <-monitorParent(ctx, 0):
		t.Fatal("disabled parent monitor closed")
	case <-ctx.Done():
	}
}

func TestSupervisorStopsEveryChildWhenContextEnds(t *testing.T) {
	if os.Getenv("JARVIS_APP_SERVICE_TEST_CHILD") != "" {
		runTestHTTPChild()
		return
	}
	resourceRoot := t.TempDir()
	stateRoot := t.TempDir()
	writeBundleFixture(t, resourceRoot)
	serverAddress := freeAddress(t)
	qdrantAddress := freeAddress(t)
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writeTestChild(t, filepath.Join(resourceRoot, "bin", "jarvis-server"), testBinary, serverAddress)
	writeTestChild(t, filepath.Join(resourceRoot, "bin", "qdrant"), testBinary, qdrantAddress)
	service, err := New(Options{
		ResourceRoot:    resourceRoot,
		StateRoot:       stateRoot,
		Address:         serverAddress,
		QdrantHealthURL: "http://" + qdrantAddress + "/healthz",
		StartupTimeout:  20 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	var output safeBuffer
	go func() {
		result <- service.Run(ctx, &output)
	}()
	waitForTestHTTP(t, "http://"+serverAddress+"/healthz")
	waitForOutput(t, &output, RuntimeConnectionPrefix)
	if !strings.Contains(output.String(), RuntimeConnectionPrefix) {
		t.Fatalf("runtime connection was not announced: %q", output.String())
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() after cancellation = %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("app service did not stop")
	}
	assertAddressAvailable(t, serverAddress)
	assertAddressAvailable(t, qdrantAddress)
}

func TestSupervisorStartsCCConnectAndRestartsServerAfterOnboarding(t *testing.T) {
	resourceRoot := t.TempDir()
	stateRoot := t.TempDir()
	writeBundleFixture(t, resourceRoot)
	writeConnectionConfigFixture(t, filepath.Join(resourceRoot, "conf", "config.yaml"))
	t.Setenv("JARVIS_API_BASE", "http://stale-parent:1")
	t.Setenv("JARVIS_TIMEZONE", "stale-parent-timezone")
	serverAddress := freeAddress(t)
	qdrantAddress := freeAddress(t)
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	serverStarts := filepath.Join(t.TempDir(), "server-starts")
	ccStarted := filepath.Join(t.TempDir(), "cc-started")
	writeRestartableTestChild(
		t,
		filepath.Join(resourceRoot, "bin", "jarvis-server"),
		testBinary,
		serverAddress,
		serverStarts,
	)
	writeTestChild(t, filepath.Join(resourceRoot, "bin", "qdrant"), testBinary, qdrantAddress)
	writeLongRunningChild(t, filepath.Join(resourceRoot, "bin", "cc-connect-jarvis"), ccStarted)
	service, err := New(Options{
		ResourceRoot:    resourceRoot,
		StateRoot:       stateRoot,
		Address:         serverAddress,
		QdrantHealthURL: "http://" + qdrantAddress + "/healthz",
		StartupTimeout:  20 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- service.Run(ctx, &safeBuffer{})
	}()
	waitForTestHTTP(t, "http://"+serverAddress+"/healthz")
	waitForFileLines(t, serverStarts, 1)

	if err := os.MkdirAll(filepath.Dir(service.layout.CCConnectConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(filepath.Dir(service.layout.ConfigPath), "config.runtime.yaml")
	if err := os.WriteFile(overridePath, []byte("server:\n  addr: 0.0.0.0:18899\ncapture:\n  timezone: Asia/Tokyo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.layout.CCConnectConfig, []byte("[test]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFileLines(t, ccStarted, 1)
	if got := readFile(t, ccStarted); got != "http://"+serverAddress+" Asia/Tokyo\n" {
		t.Fatalf("first CC environment = %q", got)
	}
	if err := os.WriteFile(overridePath, []byte("capture:\n  timezone: UTC\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(service.layout.RestartRequestPath, []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFileLines(t, serverStarts, 2)
	waitForFileLines(t, ccStarted, 2)
	if got := readFile(t, ccStarted); got != "http://"+serverAddress+" Asia/Tokyo\nhttp://"+serverAddress+" UTC\n" {
		t.Fatalf("restarted CC environment = %q", got)
	}
	waitForTestHTTP(t, "http://"+serverAddress+"/healthz")

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() after cancellation = %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("app service did not stop")
	}
}

type safeBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *safeBuffer) Write(raw []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(raw)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func runTestHTTPChild() {
	address := os.Getenv("JARVIS_APP_SERVICE_TEST_CHILD")
	if logPath := os.Getenv("JARVIS_APP_SERVICE_TEST_START_LOG"); logPath != "" {
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(1)
		}
		_, _ = file.WriteString("started\n")
		_ = file.Close()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	if err := http.ListenAndServe(address, mux); err != nil {
		os.Exit(1)
	}
}

func writeTestChild(t *testing.T, path, testBinary, address string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\nJARVIS_APP_SERVICE_TEST_CHILD=%q exec %q -test.run=TestSupervisorStopsEveryChildWhenContextEnds\n",
		address,
		testBinary,
	)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeRestartableTestChild(t *testing.T, path, testBinary, address, startLog string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\nJARVIS_APP_SERVICE_TEST_CHILD=%q JARVIS_APP_SERVICE_TEST_START_LOG=%q exec %q -test.run=TestSupervisorStopsEveryChildWhenContextEnds\n",
		address,
		startLog,
		testBinary,
	)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeLongRunningChild(t *testing.T, path, startedPath string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s %%s\\n' \"$JARVIS_API_BASE\" \"$JARVIS_TIMEZONE\" >> %q\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n",
		startedPath,
	)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func waitForTestHTTP(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			response.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("test HTTP server did not start: %s", url)
}

func waitForOutput(t *testing.T, output *safeBuffer, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), text) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("output did not contain %q: %q", text, output.String())
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file was not created: %s", path)
}

func waitForFileLines(t *testing.T, path string, count int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil && strings.Count(string(raw), "\n") >= count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s did not reach %d lines", path, count)
}

func assertAddressAvailable(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("address %s is still in use: %v", address, err)
	}
	listener.Close()
}

func writeConnectionConfigFixture(t *testing.T, path string) {
	t.Helper()
	const fixture = `
identity: {display_name: Jarvis}
server: {addr: '0.0.0.0:18800', web_root: web/dist}
sqlite: {path: var/jarvis.db}
factengine: {schedule: '@every 15m', bin: mock, model: mock, reasoning_effort: low, sandbox: danger-full-access, timeout_sec: 300, batch_limit: 200, max_material_chars: 100000, window_gap_minutes: 30, window_max_messages: 40}
proactive: &agent {schedule: '@every 1h', startup_delay_seconds: 120, bin: mock, model: mock, reasoning_effort: low, sandbox: danger-full-access, timeout_seconds: 900}
meeting_sweep: *agent
morning_brief: *agent
extract: {schedule: '@every 10m', engine: codex, codex_reasoning_effort: low, codex_sandbox: danger-full-access, concurrency: 2, batch_messages: 400, context_messages: 20, context_window_minutes: 120, open_todo_limit: 50, recent_task_limit: 10, max_prompt_chars: 60000, semantic_collection: todo_semantic, semantic_threshold: 0.85, semantic_neighbor_limit: 3, tool_timeout_sec: 10, history_tool_limit: 50, qdrant_host: 127.0.0.1, qdrant_grpc_port: 6334}
lark_cli: {bin: mock, rate_limit: 5, burst: 10, concurrent: 2, timeout_sec: 60}
capture: {page_size: 50, scan_workers: 2, hot_age_hours: 6, warm_age_hours: 168, timezone: Asia/Shanghai, discover_schedule: '@every 6h', scan_schedule: '@every 5m', p2p_activation_window_minutes: 15}
codex: {bin: mock, model: mock, timeout_seconds: 600}
execute: {schedule: '@every 5m', repo_root: ., runs_dir: runs, bin: mock, model: mock, reasoning_effort: low, timeout_second: 1800, stale_executing_minute: 45}
chat: {timeout_seconds: 600, reasoning_effort: low, sandbox: danger-full-access}
skills: {root: .agents/skills}
dailydigest: {schedule: '0 19 * * *', timeout_seconds: 600, git_author: mock, group_message_limit: 200, group_concurrency: 2}
scheduled_task: {schedule: '@every 1m', batch_limit: 20}
`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("invalid connection fixture: %v", err)
	}
}

func writeBundleFixture(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"bin/jarvis-server":         "#!/bin/sh\n",
		"bin/qdrant":                "#!/bin/sh\n",
		"bin/cc-connect-jarvis":     "#!/bin/sh\n",
		"conf/config.yaml":          "version: 1\n",
		"conf/qdrant.yaml":          "service: {}\n",
		"conf/prompts/m3.md":        "prompt-v1\n",
		".agents/skills/a/SKILL.md": "# A\n",
		"scripts/jarvis-tools":      "#!/bin/sh\n",
		"web/dist/index.html":       "web-v1\n",
	}
	files["scripts/json-api-data.mjs"] = "export {};\n"
	files["scripts/lib/connection.sh"] = "#!/bin/bash\n"
	for _, module := range []string{"common", "commands", "world", "evidence", "task", "schedule", "memory", "skill", "notify"} {
		files["scripts/lib/jarvis-tools/"+module+".sh"] = "#!/bin/bash\n"
	}
	for relative, content := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(relative, "bin/") || strings.HasPrefix(relative, "scripts/") {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func contextWithTimeout(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), timeout)
}
