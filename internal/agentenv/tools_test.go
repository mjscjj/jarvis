package agentenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureToolsPrependsExecutableRuntimeScript(t *testing.T) {
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "jarvis-tools"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", strings.Join([]string{"/usr/bin", scriptsDir, "/bin"}, string(os.PathListSeparator)))

	if err := ConfigureTools(root); err != nil {
		t.Fatalf("ConfigureTools: %v", err)
	}
	parts := filepath.SplitList(os.Getenv("PATH"))
	if len(parts) != 3 || parts[0] != scriptsDir || parts[1] != "/usr/bin" || parts[2] != "/bin" {
		t.Fatalf("PATH = %#v", parts)
	}
	resolved, err := exec.LookPath("jarvis-tools")
	if err != nil {
		t.Fatalf("LookPath(jarvis-tools): %v", err)
	}
	if resolved != filepath.Join(scriptsDir, "jarvis-tools") {
		t.Fatalf("jarvis-tools path = %q", resolved)
	}
}

func TestConfigureToolsRejectsMissingOrNonExecutableScript(t *testing.T) {
	root := t.TempDir()
	if err := ConfigureTools(root); err == nil || !strings.Contains(err.Error(), "stat agent tool") {
		t.Fatalf("missing tool error = %v", err)
	}
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "jarvis-tools"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureTools(root); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("non-executable tool error = %v", err)
	}
}
