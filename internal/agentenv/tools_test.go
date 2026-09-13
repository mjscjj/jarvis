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
	t.Setenv("JARVIS_API_BASE", "http://old-instance:1")
	t.Setenv("JARVIS_TIMEZONE", "UTC")

	if err := ConfigureTools(root, "0.0.0.0:18811", "Asia/Tokyo"); err != nil {
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
	output, err := exec.Command("/bin/sh", "-c", `printf '%s\n%s\n' "$JARVIS_API_BASE" "$JARVIS_TIMEZONE"`).CombinedOutput()
	if err != nil || string(output) != "http://127.0.0.1:18811\nAsia/Tokyo\n" {
		t.Fatalf("inherited connection = %q, error = %v", output, err)
	}
	if err := ConfigureTools(root, "[::]:18812", "UTC"); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("JARVIS_API_BASE"); got != "http://[::1]:18812" {
		t.Fatalf("reconfigured API = %q", got)
	}
	if got := os.Getenv("JARVIS_TIMEZONE"); got != "UTC" {
		t.Fatalf("reconfigured timezone = %q", got)
	}
}

func TestLocalAPIBase(t *testing.T) {
	for _, tc := range []struct{ address, want string }{
		{"0.0.0.0:18800", "http://127.0.0.1:18800"},
		{":18801", "http://127.0.0.1:18801"},
		{"[::]:18802", "http://[::1]:18802"},
		{"[::1]:18803", "http://[::1]:18803"},
		{"[2001:db8::1]:18804", "http://[2001:db8::1]:18804"},
		{"[fe80::1%lo0]:18805", "http://[fe80::1%25lo0]:18805"},
		{" localhost:00123 ", "http://localhost:123"},
		{"127.0.0.2:65535", "http://127.0.0.2:65535"},
	} {
		t.Run(tc.address, func(t *testing.T) {
			got, err := LocalAPIBase(tc.address)
			if err != nil || got != tc.want {
				t.Fatalf("LocalAPIBase(%q) = %q, %v; want %q", tc.address, got, err, tc.want)
			}
		})
	}
	for _, address := range []string{"", "localhost", "http://localhost:18800", "::1:18800", ":0", ":-1", ":65536", ":http", ":"} {
		if got, err := LocalAPIBase(address); err == nil {
			t.Errorf("LocalAPIBase(%q) = %q, wanted an error", address, got)
		}
	}
}

func TestConfigureToolsRejectsMissingOrNonExecutableScript(t *testing.T) {
	root := t.TempDir()
	if err := ConfigureTools(root, "0.0.0.0:18811", "Asia/Tokyo"); err == nil || !strings.Contains(err.Error(), "stat agent tool") {
		t.Fatalf("missing tool error = %v", err)
	}
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "jarvis-tools"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureTools(root, "0.0.0.0:18811", "Asia/Tokyo"); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("non-executable tool error = %v", err)
	}
}
