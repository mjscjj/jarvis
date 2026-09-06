package appmodule

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceListsAndUpdatesKnownModule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modules.yaml")
	if err := os.WriteFile(path, []byte("modules:\n  - key: okr\n    enabled: true\n  - key: biz-okr\n    enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(path)
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.List(context.Background())
	if err != nil || len(items) != 2 || items[0].Key != "biz-okr" || len(items[0].Requires) != 1 || items[1].Key != "okr" || !items[1].IsEnabled || items[1].Requires == nil {
		t.Fatalf("List() items=%+v err=%v", items, err)
	}
	if items[0].Name != "OKR" || items[1].Name != "OKR 插件" {
		t.Fatalf("List() names=(%q, %q), want OKR and OKR 插件", items[0].Name, items[1].Name)
	}
	disabled := false
	updated, err := service.Update(context.Background(), "biz-okr", Input{IsEnabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.IsEnabled || updated.ConfiguredEnabled || !updated.RestartRequired {
		t.Fatalf("Update() = %+v, want active now and disabled after restart", updated)
	}
	enabled, err := service.Enabled(context.Background(), "biz-okr")
	if err != nil || !enabled {
		t.Fatalf("Enabled(biz-okr) = %t, %v, want current runtime true", enabled, err)
	}
	reloaded, err := NewService(path)
	if err != nil {
		t.Fatal(err)
	}
	items, err = reloaded.List(context.Background())
	if err != nil || len(items) != 2 || items[0].IsEnabled || items[0].ConfiguredEnabled || items[0].RestartRequired {
		t.Fatalf("reloaded List() items=%+v err=%v", items, err)
	}
}

func TestServiceRejectsMissingBuiltInModule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modules.yaml")
	if err := os.WriteFile(path, []byte("modules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(path); err == nil {
		t.Fatal("NewService() error=nil, want missing module failure")
	}
}

func TestServiceRejectsEnabledModuleWithDisabledDependency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modules.yaml")
	if err := os.WriteFile(path, []byte("modules:\n  - key: okr\n    enabled: false\n  - key: biz-okr\n    enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(path); err == nil {
		t.Fatal("NewService() error=nil, want dependency failure")
	}
}

func TestNewServiceMigratesLegacyAgencyOKRKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modules.yaml")
	if err := os.WriteFile(path, []byte("modules:\n  - key: okr\n    enabled: true\n  - key: agency-okr\n    enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(path)
	if err != nil {
		t.Fatal(err)
	}
	if enabled, err := service.Enabled(t.Context(), "biz-okr"); err != nil || !enabled {
		t.Fatalf("Enabled(biz-okr) = %t, %v, want true", enabled, err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "modules:\n  - key: okr\n    enabled: true\n  - key: biz-okr\n    enabled: true\n"
	if string(content) != want {
		t.Fatalf("migrated modules.yaml = %q, want %q", content, want)
	}
}

func TestNewServiceRejectsLegacyAndCurrentBizOKRKeysTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modules.yaml")
	content := "modules:\n  - key: okr\n    enabled: true\n  - key: agency-okr\n    enabled: true\n  - key: biz-okr\n    enabled: true\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(path); err == nil {
		t.Fatal("NewService() error=nil, want duplicate migrated key failure")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("invalid config was rewritten: got %q, want %q", got, content)
	}
}
