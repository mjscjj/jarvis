package background

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"
)

func newProjectPortabilityTestService(t *testing.T) (*ProjectPortabilityService, *ProjectService, *ResourceService) {
	t.Helper()
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	projects, _ := NewProjectService(db)
	resources, _ := NewResourceService(db)
	service, err := NewProjectPortabilityService(db, projects, resources, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return service, projects, resources
}

func TestProjectExportImportAndDuplicate(t *testing.T) {
	service, projects, resources := newProjectPortabilityTestService(t)
	code := "friday"
	project, err := projects.Create(t.Context(), ProjectInput{
		Code: &code, Name: "Friday", Role: "owner", Status: "active", Priority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := "Friday 项目当前事实"
	if err := service.db.Model(&domain.Project{}).Where("id = ?", project.ID).Update("summary", summary).Error; err != nil {
		t.Fatal(err)
	}
	repoURL := "git@code.byted.org:team/friday.git"
	resource, err := resources.Create(t.Context(), ResourceInput{
		Title: "Friday Repo", ResourceType: "repo", URL: &repoURL, ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	localPath := "/private/local/friday"
	if err := service.db.Model(&domain.ManagedResource{}).Where("id = ?", resource.ID).Update("local_path", localPath).Error; err != nil {
		t.Fatal(err)
	}

	bundle, err := service.Export(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.SchemaVersion != 1 || bundle.Summary != summary || len(bundle.Resources) != 1 {
		t.Fatalf("unexpected bundle: %+v", bundle)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "local_path") || strings.Contains(string(encoded), localPath) {
		t.Fatalf("export leaked local checkout: %s", encoded)
	}
	preview, err := service.Preview(t.Context(), ProjectImportInput{Bundle: *bundle})
	if err != nil || preview.Valid || preview.Action != "conflict" {
		t.Fatalf("preview = %+v, error = %v", preview, err)
	}
	copyName := "Friday Copy"
	duplicate, err := service.Duplicate(t.Context(), project.ID, ProjectDuplicateInput{Name: copyName})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID == project.ID || duplicate.Name != copyName || duplicate.Code != nil {
		t.Fatalf("duplicate = %+v", duplicate)
	}
	var copied []domain.ManagedResource
	if err := service.db.Where("project_id = ?", duplicate.ID).Find(&copied).Error; err != nil {
		t.Fatal(err)
	}
	if len(copied) != 1 || copied[0].LocalPath != nil || copied[0].URL == nil || *copied[0].URL != repoURL {
		t.Fatalf("copied resources = %+v", copied)
	}
}

func TestResolveProjectRepositoriesUsesUniqueRemoteMatch(t *testing.T) {
	service, projects, resources := newProjectPortabilityTestService(t)
	project, err := projects.Create(t.Context(), ProjectInput{
		Name: "Codebase", Role: "participant", Status: "active", Priority: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	repoURL := "git@code.byted.org:team/service.git"
	resource, err := resources.Create(t.Context(), ResourceInput{
		Title: "Service Repo", ResourceType: "repo", URL: &repoURL, ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := t.TempDir()
	repoPath := filepath.Join(repoRoot, "service")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", repoPath},
		{"-C", repoPath, "remote", "add", "origin", "https://code.byted.org/team/service.git"},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	service.repoRoot = repoRoot
	bindings, err := service.ResolveRepositories(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].Status != "matched" || bindings[0].LocalPath == nil || *bindings[0].LocalPath != repoPath {
		t.Fatalf("bindings = %+v", bindings)
	}
	var stored domain.ManagedResource
	if err := service.db.First(&stored, resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.LocalPath == nil || *stored.LocalPath != repoPath {
		t.Fatalf("stored local path = %v", stored.LocalPath)
	}
}
