package plugin

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/skill"
)

func TestProductPluginOwnsDiscoverableSkillsWithoutCreatingSchedules(t *testing.T) {
	registry, err := BuiltinRegistry()
	if err != nil {
		t.Fatal(err)
	}
	scheduler := newFakeScheduler()
	svc, err := NewService(openPluginDB(t), registry, newAuthorizer(fakeRunner{run: func(string, []string) ([]byte, error) {
		t.Fatal("product capability must not probe external authorization")
		return nil, nil
	}}), scheduler)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := skill.NewService(filepath.Join("..", "..", ".agents", "skills"), filepath.Join("..", "..", "conf", "skills.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	catalog.SetAvailability(svc)
	if enabled, err := svc.Enabled(t.Context(), "product-management"); err != nil || enabled {
		t.Fatalf("default enabled=%v err=%v", enabled, err)
	}
	if _, err := catalog.Content(t.Context(), "product-doc-review"); !errors.Is(err, skill.ErrNotFound) {
		t.Fatalf("disabled content err=%v", err)
	}
	view, err := svc.Update(t.Context(), "product-management", UpdateInput{Enabled: true, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(scheduler.items) != 0 || scheduler.triggers != 0 {
		t.Fatal("enabling created an unsolicited task")
	}
	for _, name := range view.Skills {
		if enabled, err := catalog.Executable(t.Context(), name); err != nil || !enabled {
			t.Fatalf("skill %s not executable: %v", name, err)
		}
		body, err := catalog.Content(t.Context(), name)
		if err != nil || strings.TrimSpace(body.Content) == "" {
			t.Fatalf("skill=%s content=%+v err=%v", name, body, err)
		}
	}
	text, err := catalog.Catalog(t.Context(), skill.StageExecute)
	if err != nil || !strings.Contains(text, "get-skill --name product-doc-review") {
		t.Fatalf("execute catalog missing product skill: %v", err)
	}
	text, err = catalog.Catalog(t.Context(), skill.StageExtract)
	if err != nil || strings.Contains(text, "product-doc-review") {
		t.Fatalf("product review leaked into extract: %v", err)
	}
	if _, err := svc.Update(t.Context(), view.ID, UpdateInput{Enabled: false, ExpectedRevision: view.Revision}); err != nil {
		t.Fatal(err)
	}
	if enabled, err := svc.Enabled(t.Context(), view.ID); err != nil || enabled {
		t.Fatalf("disabled=%v err=%v", enabled, err)
	}
	if enabled, err := catalog.Executable(t.Context(), "product-doc-review"); err != nil || enabled {
		t.Fatalf("disabled plugin skill executable=%v err=%v", enabled, err)
	}
	if _, err := catalog.Executable(t.Context(), "missing"); !errors.Is(err, skill.ErrNotFound) {
		t.Fatalf("unknown skill err=%v", err)
	}
}
