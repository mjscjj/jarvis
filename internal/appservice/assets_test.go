package appservice

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/config"
)

func TestPrepareReplacesModifiedBaselineWithCurrentConfig(t *testing.T) {
	resources := t.TempDir()
	writeBundleFixture(t, resources)
	base, err := os.ReadFile("../../conf/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeAsset(t, resources, "conf/config.yaml", string(base)+"\nretired_upgrade_test_key: true\n")
	layout := NewLayout(Options{ResourceRoot: resources, StateRoot: t.TempDir()})
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	writeAsset(t, layout.RuntimeRoot, "conf/config.yaml", readFile(t, layout.ConfigPath)+"# local edit\n")
	if _, err := config.Load(layout.ConfigPath); err == nil || !strings.Contains(err.Error(), "retired_upgrade_test_key") {
		t.Fatalf("old baseline should fail current strict parsing: %v", err)
	}
	override := readFile(t, config.RuntimeOverridePath(layout.ConfigPath))
	writeAsset(t, resources, "conf/config.yaml", string(base))
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, layout.ConfigPath); got != string(base) {
		t.Fatal("modified baseline did not follow the bundle")
	}
	if _, err := config.Load(layout.ConfigPath); err != nil {
		t.Fatalf("upgraded baseline should pass strict parsing: %v", err)
	}
	if got := readFile(t, config.RuntimeOverridePath(layout.ConfigPath)); got != override {
		t.Fatal("upgrade changed local settings")
	}
}

func TestPrepareRecoversMissingManifestAndContinuesUpgrading(t *testing.T) {
	resources := t.TempDir()
	writeBundleFixture(t, resources)
	conflicts := []string{
		"conf/config.yaml", "conf/qdrant.yaml", "conf/skills.yaml",
		"conf/rules/m3.md", "conf/prompts/m3.md", ".agents/skills/a/SKILL.md",
		"scripts/jarvis-tools", "web/dist/index.html",
	}
	for _, relative := range conflicts {
		writeAsset(t, resources, relative, "v1: "+relative+"\n")
	}
	layout := NewLayout(Options{ResourceRoot: resources, StateRoot: t.TempDir()})
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	if backups := assetBackups(t, layout); len(backups) != 0 {
		t.Fatalf("fresh installation created backups: %v", backups)
	}
	writeAsset(t, layout.RuntimeRoot, "conf/prompts/m3.md", "user prompt\n")
	writeAsset(t, layout.RuntimeRoot, ".agents/skills/custom/SKILL.md", "user skill\n")
	writeAsset(t, layout.RuntimeRoot, "data/shared-memory.md", "user memory\n")
	writeAsset(t, layout.RuntimeRoot, "conf/config.runtime.yaml", "identity:\n  display_name: Friday\n")
	// A runtime overlay accidentally present in the bundle must still be skipped.
	writeAsset(t, resources, "conf/config.runtime.yaml", "do not install\n")
	if err := os.Remove(filepath.Join(layout.StateRoot, assetManifestFilename)); err != nil {
		t.Fatal(err)
	}
	for _, relative := range conflicts {
		writeAsset(t, resources, relative, "v2: "+relative+"\n")
	}
	var diagnostics bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&diagnostics)
	t.Cleanup(func() { log.SetOutput(previousLogOutput) })
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	backups := assetBackups(t, layout)
	if len(backups) != 1 {
		t.Fatalf("expected one recovery backup, got %v", backups)
	}
	for _, relative := range conflicts {
		if !strings.Contains(diagnostics.String(), "restored bundled runtime asset \""+relative+"\"") ||
			!strings.Contains(diagnostics.String(), filepath.Join(backups[0], relative)) {
			t.Errorf("missing recovery diagnostics for %s", relative)
		}
		wantOld := "v1: " + relative + "\n"
		if relative == "conf/prompts/m3.md" {
			wantOld = "user prompt\n"
		}
		if got := readFile(t, filepath.Join(backups[0], relative)); got != wantOld {
			t.Errorf("backup %s = %q, want %q", relative, got, wantOld)
		}
		if got := readFile(t, filepath.Join(layout.RuntimeRoot, relative)); got != "v2: "+relative+"\n" {
			t.Errorf("asset not restored: %s = %q", relative, got)
		}
	}
	for relative, want := range map[string]string{
		"conf/config.runtime.yaml":       "identity:\n  display_name: Friday\n",
		".agents/skills/custom/SKILL.md": "user skill\n",
		"data/shared-memory.md":          "user memory\n",
	} {
		if got := readFile(t, filepath.Join(layout.RuntimeRoot, relative)); got != want {
			t.Errorf("recovery changed user file %s: %q", relative, got)
		}
		if _, err := os.Stat(filepath.Join(backups[0], relative)); !os.IsNotExist(err) {
			t.Errorf("user-only file was included in recovery: %s, %v", relative, err)
		}
	}
	if _, err := os.Stat(filepath.Join(backups[0], "scripts/json-api-data.mjs")); !os.IsNotExist(err) {
		t.Fatalf("identical asset should not be backed up: %v", err)
	}
	// A restart must be idempotent; the next release must upgrade restored prompts.
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	writeAsset(t, resources, "conf/prompts/m3.md", "prompt-v3\n")
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(layout.RuntimeRoot, "conf/prompts/m3.md")); got != "prompt-v3\n" {
		t.Fatalf("recovered prompt remained frozen: %q", got)
	}
	if got := assetBackups(t, layout); len(got) != 1 {
		t.Fatalf("restart created unnecessary backups: %v", got)
	}
}

func TestPrepareRejectsCorruptManifestWithoutOverwritingAssets(t *testing.T) {
	resources := t.TempDir()
	writeBundleFixture(t, resources)
	layout := NewLayout(Options{ResourceRoot: resources, StateRoot: t.TempDir()})
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	writeAsset(t, layout.StateRoot, assetManifestFilename, "{broken")
	writeAsset(t, resources, "conf/prompts/m3.md", "prompt-v2\n")
	if err := Prepare(layout); err == nil || !strings.Contains(err.Error(), "decode asset manifest") {
		t.Fatalf("corrupt manifest should fail explicitly: %v", err)
	}
	if got := readFile(t, filepath.Join(layout.RuntimeRoot, "conf/prompts/m3.md")); got != "prompt-v1\n" {
		t.Fatalf("failed recovery changed prompt: %q", got)
	}
}

func TestSyncStopsBeforeOverwriteWhenBackupCannotBeCreated(t *testing.T) {
	resources := t.TempDir()
	writeBundleFixture(t, resources)
	layout := NewLayout(Options{ResourceRoot: resources, StateRoot: t.TempDir()})
	if err := Prepare(layout); err != nil {
		t.Fatal(err)
	}
	writeAsset(t, resources, "conf/prompts/m3.md", "prompt-v2\n")
	// Keep the installed runtime, but make the manifest/backup parent unavailable.
	layout.StateRoot = filepath.Join(t.TempDir(), "missing-parent")
	if err := syncRuntimeAssets(layout); err == nil || !strings.Contains(err.Error(), "backup directory") {
		t.Fatalf("backup failure must stop synchronization: %v", err)
	}
	if got := readFile(t, filepath.Join(layout.RuntimeRoot, "conf/prompts/m3.md")); got != "prompt-v1\n" {
		t.Fatalf("backup failure lost the original: %q", got)
	}
	if _, err := os.Stat(filepath.Join(layout.StateRoot, assetManifestFilename)); !os.IsNotExist(err) {
		t.Fatalf("failed synchronization wrote a new manifest: %v", err)
	}
}

func writeAsset(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assetBackups(t *testing.T, layout Layout) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(layout.StateRoot, "asset-backup-*"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}
