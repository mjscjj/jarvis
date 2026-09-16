package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstanceLabelSurvivesPortChangeAndSeparatesConfigs(t *testing.T) {
	paths := []string{filepath.Join(t.TempDir(), "config.yaml"), filepath.Join(t.TempDir(), "config.yaml")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("server:\n  addr: 0.0.0.0:18800\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := InspectInstance(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	b, err := InspectInstance(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	if a.LaunchdLabel == b.LaunchdLabel {
		t.Fatal("different configurations share launchd identity")
	}
	if err := os.WriteFile(RuntimeOverridePath(paths[0]), []byte("server:\n  addr: 0.0.0.0:18802\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := InspectInstance(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if changed.LaunchdLabel != a.LaunchdLabel || changed.APIBase != "http://127.0.0.1:18802" {
		t.Fatalf("changed instance = %+v", changed)
	}
}

func TestExportToolEnvironmentOverridesParentInstance(t *testing.T) {
	t.Setenv("JARVIS_API_BASE", "http://wrong-parent:9999")
	t.Setenv("JARVIS_CONFIG", "/wrong/parent.yaml")
	t.Setenv("PATH", "/usr/bin:/bin")
	root := t.TempDir()
	path := filepath.Join(root, "conf", "config.yaml")
	cfg := Config{Server: ServerConfig{Addr: "0.0.0.0:18802"}}
	if err := cfg.ExportToolEnvironment(path, root); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("JARVIS_API_BASE") != "http://127.0.0.1:18802" || os.Getenv("JARVIS_CONFIG") != path {
		t.Fatal("parent instance leaked into child environment")
	}
	if !strings.HasPrefix(os.Getenv("PATH"), filepath.Join(root, "scripts")+":") {
		t.Fatal("repository tools are not first in PATH")
	}
}

func TestInstanceDerivesBrowserPrefixWithoutChangingLocalAPI(t *testing.T) {
	for _, base := range []string{"/", "/dev/", "/sandbox/emily/"} {
		file := filepath.Join(t.TempDir(), "config.yaml")
		content := "server:\n  addr: 127.0.0.1:18812\n  public_base_url: https://example.com" + base + "\n"
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		instance, err := InspectInstance(file)
		if err != nil {
			t.Fatal(err)
		}
		if instance.WebBasePath != base || instance.APIBase != "http://127.0.0.1:18812" {
			t.Fatalf("unexpected instance: %+v", instance)
		}
	}
}

func TestInstanceNavigationUsesSelectedRuntimeConfig(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte("server:\n  addr: 127.0.0.1:18802\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RuntimeOverridePath(file), []byte("server:\n  okr_entry_path: /dev/\n  main_workbench_path: /\n"), 0600); err != nil {
		t.Fatal(err)
	}
	instance, err := InspectInstance(file)
	if err != nil {
		t.Fatal(err)
	}
	if instance.OKREntryPath != "/dev/" || instance.MainWorkbenchPath != "/" {
		t.Fatalf("unexpected navigation: %+v", instance)
	}
	for _, value := range []string{"https://example.com/", "//example.com/", "/dev", "/../dev/", "/dev/?q=1", "/dev/#x", "/%64ev/"} {
		if err := (ServerConfig{OKREntryPath: value}).ValidateNavigationPaths(); err == nil {
			t.Fatalf("accepted invalid installation path %q", value)
		}
	}
}
