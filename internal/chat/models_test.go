package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseTRAEModelsPreservesSourceOrderAndCapabilities(t *testing.T) {
	t.Parallel()
	models, err := parseTRAEModels([]byte(`[
  {"name":"Doubao-Seed-2.1-Pro","real_name":"Seed-2.1-Pro","description":"200K context window","supported_mime_types":["image/*"]},
  {"name":"gpt-5.6-sol","description":"reasoning","supported_mime_types":[]}
]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "Doubao-Seed-2.1-Pro" || models[0].Name != "Seed-2.1-Pro" || !models[0].Default {
		t.Fatalf("models = %#v", models)
	}
	if len(models[0].InputModalities) != 2 || models[0].InputModalities[1] != "image" {
		t.Fatalf("image capabilities = %#v", models[0].InputModalities)
	}
	if models[1].Name != "gpt-5.6-sol" || models[1].Default {
		t.Fatalf("fallback model = %#v", models[1])
	}
}

func TestParseCursorModelsIgnoresHeadingsAndFindsAuto(t *testing.T) {
	t.Parallel()
	models, err := parseCursorModels("Available models\n\nauto - Auto (default)\ngpt-5.6-sol-high - GPT-5.6 Sol 1M High\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "auto" || !models[0].Default || models[1].ID != "gpt-5.6-sol-high" {
		t.Fatalf("models = %#v", models)
	}
}

func TestParseCursorModelsFailsOnProtocolDrift(t *testing.T) {
	t.Parallel()
	if _, err := parseCursorModels("Available models\n(no model lines)\n"); err == nil {
		t.Fatal("parseCursorModels() must fail when the upstream format has no model rows")
	}
}

func TestAgentSelectionRejectsMislabeledCommandWithoutSearchingPastIt(t *testing.T) {
	wrapperDir, realDir := t.TempDir(), t.TempDir()
	writeExecutable(t, wrapperDir, "codex", "#!/bin/sh\nprintf '%s\\n' 'traecli 0.200.19'\n")
	writeExecutable(t, realDir, "codex", "#!/bin/sh\nprintf '%s\\n' 'codex-cli 0.153.4'\n")
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+realDir)
	svc := &Service{runner: &runner{agent: "codex"}}
	if view := svc.ListAgents(t.Context())[0]; view.Available || !strings.Contains(view.Error, "reports trae") {
		t.Fatalf("mislabeled agent: %+v", view)
	}
	if _, err := svc.ListModels(t.Context(), "codex"); err == nil || !strings.Contains(err.Error(), "reports trae") {
		t.Fatalf("model discovery must reject the wrapper: %v", err)
	}
	if _, err := newAgentRunner(t.Context(), "codex", "test", "danger-full-access", "high", 1); err == nil || !strings.Contains(err.Error(), "reports trae") {
		t.Fatalf("execution must reject the wrapper: %v", err)
	}
}

func TestCursorBuildVersionWorksForDiscoveryAndExecution(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, dir, "cursor-agent", `#!/bin/sh
case "$1" in
 --version) printf '%s\n' '2026.09.10-fd3934a';;
 --list-models) printf '%s\n' 'auto - Auto (default)';;
 *) printf '%s\n' '{"type":"system","session_id":"cursor-test"}' '{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}';;
esac
`)
	t.Setenv("PATH", dir)
	svc := &Service{runner: &runner{agent: "cursor"}}
	if view := svc.ListAgents(t.Context())[2]; !view.Available || view.Version != "2026.09.10-fd3934a" {
		t.Fatalf("Cursor unavailable: %+v", view)
	}
	models, err := svc.ListModels(t.Context(), "cursor")
	if err != nil || len(models) != 1 || models[0].ID != "auto" {
		t.Fatalf("models: %+v, %v", models, err)
	}
	r, err := newAgentRunner(t.Context(), "cursor", "auto", "danger-full-access", "high", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var reply string
	err = r.Stream(t.Context(), "hello", "", nil, func(e Event) error { reply += e.Text; return nil })
	if err != nil || reply != "hello" {
		t.Fatalf("reply: %q, %v", reply, err)
	}
}

func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
