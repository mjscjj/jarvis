package insight

import (
	"testing"
	"time"
)

func TestBuildAgentProcessSnapshotCountsLogicalInstances(t *testing.T) {
	output := `
  100     1   100       10:00 /opt/jarvis/bin/jarvis-server
  200   100   200       01:20 node /Users/me/.local/bin/codex exec --json -
  201   200   200       01:20 /Users/me/vendor/bin/codex exec --json -
  202   201   202       01:19 /Users/me/vendor/bin/codex-code-mode-host
  300     1   300       08:00 node /Users/me/.local/bin/codex app-server
  301   300   300       08:00 /Users/me/vendor/bin/codex app-server
  400     1   400       20:00 /Applications/Trae CN.app/Contents/MacOS/Electron
  401   400   400       20:00 /Applications/Trae CN.app/Contents/Frameworks/Trae CN Helper.app/Contents/MacOS/Trae CN Helper --type=utility
  500   100   500       00:30 /Users/me/.local/bin/traex run task
`
	snapshot := buildAgentProcessSnapshot(output, 100, time.Date(2026, 7, 25, 20, 0, 0, 0, time.UTC))

	if snapshot.Summary.CodexServices != 1 || snapshot.Summary.CodexExecuting != 1 || snapshot.Summary.JarvisCodex != 1 {
		t.Fatalf("Codex summary = %+v, want services=1 executing=1 jarvis=1", snapshot.Summary)
	}
	if snapshot.Summary.TraeDesktop != 1 || snapshot.Summary.TraeCLI != 1 || snapshot.Summary.JarvisTrae != 1 {
		t.Fatalf("Trae summary = %+v, want desktop=1 cli=1 jarvis=1", snapshot.Summary)
	}
	if len(snapshot.Items) != 4 {
		t.Fatalf("items len = %d, want 4: %+v", len(snapshot.Items), snapshot.Items)
	}
	for _, item := range snapshot.Items {
		if item.PID == 200 || item.PID == 202 || item.PID == 300 || item.PID == 401 {
			t.Fatalf("launcher/helper PID %d was counted: %+v", item.PID, item)
		}
	}
}

func TestBuildAgentProcessSnapshotCollapsesNestedAppServers(t *testing.T) {
	output := `
  100     1   100       10:00 /Users/me/.local/lib/node_modules/cc-connect/bin/cc-connect
  200   100   100       09:59 node /Users/me/.local/bin/codex app-server
  201   200   100       09:59 /Users/me/vendor/bin/codex app-server
  300   201   300       01:00 /Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl
  301   300   300       00:59 /Applications/ChatGPT.app/Contents/Resources/codex app-server --listen stdio://
`
	snapshot := buildAgentProcessSnapshot(output, 999, time.Date(2026, 7, 25, 20, 0, 0, 0, time.UTC))

	if snapshot.Summary.CodexServices != 1 {
		t.Fatalf("Codex services = %d, want 1: %+v", snapshot.Summary.CodexServices, snapshot.Items)
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("items len = %d, want 2: %+v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].RootPID != 201 || snapshot.Items[0].Nested {
		t.Fatalf("root item = %+v, want root_pid=201 nested=false", snapshot.Items[0])
	}
	if snapshot.Items[1].RootPID != 201 || !snapshot.Items[1].Nested {
		t.Fatalf("nested item = %+v, want root_pid=201 nested=true", snapshot.Items[1])
	}
	for _, item := range snapshot.Items {
		if item.Source != "cc-connect" {
			t.Fatalf("source = %q, want cc-connect: %+v", item.Source, item)
		}
	}
}

func TestBuildAgentProcessSnapshotIdentifiesSources(t *testing.T) {
	output := `
  100     1   100       10:00 /Applications/ChatGPT.app/Contents/MacOS/ChatGPT
  101   100   100       09:59 /Applications/ChatGPT.app/Contents/Resources/codex app-server
  200     1   200       08:00 Paseo Daemon
  201   200   200       07:59 node /Users/me/.local/bin/codex app-server
  202   201   200       07:59 /Users/me/vendor/bin/codex app-server
`
	snapshot := buildAgentProcessSnapshot(output, 999, time.Date(2026, 7, 25, 20, 0, 0, 0, time.UTC))

	if len(snapshot.Items) != 2 {
		t.Fatalf("items len = %d, want 2: %+v", len(snapshot.Items), snapshot.Items)
	}
	if snapshot.Items[0].Source != "chatgpt" || snapshot.Items[1].Source != "paseo" {
		t.Fatalf("sources = %q/%q, want chatgpt/paseo", snapshot.Items[0].Source, snapshot.Items[1].Source)
	}
}

func TestClassifyAgentProcessAcceptsChatGPTCodexOptions(t *testing.T) {
	kind, mode, ok := classifyAgentProcess(`/Applications/ChatGPT.app/Contents/Resources/codex -c features.code_mode_host=true app-server --analytics-default-enabled`)
	if !ok || kind != "codex" || mode != "app-server" {
		t.Fatalf("classification = %q/%q/%v", kind, mode, ok)
	}
}

func TestDescendsFromStopsOnCycle(t *testing.T) {
	if descendsFrom(2, 10, map[int]int{2: 3, 3: 2}) {
		t.Fatal("cycle was classified as a descendant")
	}
}
