package background

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

const ProjectBundleSchemaVersion = 1

type SharedProjectResource struct {
	Title        string  `json:"title"`
	ResourceType string  `json:"resource_type"`
	URL          *string `json:"url"`
	Summary      *string `json:"summary"`
	IsActive     bool    `json:"is_active"`
}

type ProjectBundle struct {
	SchemaVersion int                     `json:"schema_version"`
	Project       ProjectInput            `json:"project"`
	Summary       string                  `json:"summary"`
	Resources     []SharedProjectResource `json:"resources"`
	ExportedAt    time.Time               `json:"exported_at"`
}

type ProjectImportInput struct {
	Bundle ProjectBundle `json:"bundle"`
	Name   *string       `json:"name"`
	Code   *string       `json:"code"`
}

type ProjectImportPreview struct {
	Valid             bool     `json:"valid"`
	Action            string   `json:"action"`
	ExistingProjectID *uint64  `json:"existing_project_id"`
	Warnings          []string `json:"warnings"`
}

type ProjectDuplicateInput struct {
	Name string  `json:"name"`
	Code *string `json:"code"`
}

type RepositoryBinding struct {
	ResourceID uint64  `json:"resource_id"`
	Title      string  `json:"title"`
	RemoteURL  string  `json:"remote_url"`
	LocalPath  *string `json:"local_path"`
	Status     string  `json:"status"`
}

type ProjectPortabilityService struct {
	db        *gorm.DB
	projects  *ProjectService
	resources *ResourceService
	repoRoot  string
	now       func() time.Time
}

func NewProjectPortabilityService(db *gorm.DB, projects *ProjectService, resources *ResourceService, repoRoot string) (*ProjectPortabilityService, error) {
	if db == nil || projects == nil || resources == nil {
		return nil, fmt.Errorf("project portability dependencies are required")
	}
	if strings.TrimSpace(repoRoot) == "" {
		return nil, fmt.Errorf("repository root is required")
	}
	absolute, err := filepath.Abs(strings.TrimSpace(repoRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	return &ProjectPortabilityService{
		db: db, projects: projects, resources: resources,
		repoRoot: absolute, now: time.Now,
	}, nil
}

func (s *ProjectPortabilityService) Export(ctx context.Context, projectID uint64) (*ProjectBundle, error) {
	project, err := s.projects.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var rows []domain.ManagedResource
	if err := s.db.WithContext(ctx).Where("project_id = ?", projectID).Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list project resources: %w", err)
	}
	resources := make([]SharedProjectResource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, SharedProjectResource{
			Title: row.Title, ResourceType: row.ResourceType, URL: row.URL,
			Summary: row.Summary, IsActive: row.IsActive,
		})
	}
	return &ProjectBundle{
		SchemaVersion: ProjectBundleSchemaVersion,
		Project: ProjectInput{
			Code: project.Code, Name: project.Name, Role: project.Role,
			Status: project.Status, Priority: project.Priority,
		},
		Summary: stringValue(project.Summary), Resources: resources,
		ExportedAt: s.now().UTC(),
	}, nil
}

func (s *ProjectPortabilityService) Preview(ctx context.Context, in ProjectImportInput) (*ProjectImportPreview, error) {
	project, err := importedProjectInput(in)
	if err != nil {
		return nil, err
	}
	preview := &ProjectImportPreview{Valid: true, Action: "create", Warnings: []string{}}
	if project.Code != nil {
		var existing domain.Project
		result := s.db.WithContext(ctx).Where("code = ?", *project.Code).Limit(1).Find(&existing)
		if result.Error != nil {
			return nil, fmt.Errorf("check imported project code: %w", result.Error)
		}
		if result.RowsAffected > 0 {
			preview.Valid = false
			preview.Action = "conflict"
			preview.ExistingProjectID = &existing.ID
			preview.Warnings = append(preview.Warnings, "项目代号已存在，请修改代号后导入。")
		}
	}
	var activeCount int64
	if err := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).
		Where("is_active = ?", true).Count(&activeCount).Error; err != nil {
		return nil, fmt.Errorf("count active resources before import: %w", err)
	}
	importedActive := int64(0)
	for _, resource := range in.Bundle.Resources {
		if resource.IsActive {
			importedActive++
		}
	}
	if activeCount+importedActive > maxActiveManagedResources {
		preview.Valid = false
		preview.Action = "conflict"
		preview.Warnings = append(preview.Warnings, "导入后会超过启用资源数量上限，请先停用部分资源。")
	}
	return preview, nil
}

func (s *ProjectPortabilityService) Import(ctx context.Context, in ProjectImportInput) (*ProjectView, error) {
	preview, err := s.Preview(ctx, in)
	if err != nil {
		return nil, err
	}
	if !preview.Valid {
		return nil, fmt.Errorf("%w: project import conflicts with existing project", ErrConflict)
	}
	projectInput, _ := importedProjectInput(in)
	project, err := s.projects.Create(ctx, projectInput)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Bundle.Summary) != "" {
		now := s.now().UTC()
		if err := s.db.WithContext(ctx).Model(&domain.Project{}).Where("id = ?", project.ID).
			Updates(map[string]any{"summary": in.Bundle.Summary, "last_progress_at": now}).Error; err != nil {
			return nil, fmt.Errorf("restore imported project summary: %w", err)
		}
	}
	for _, resource := range in.Bundle.Resources {
		active := resource.IsActive
		created, err := s.resources.Create(ctx, ResourceInput{
			Title: resource.Title, ResourceType: resource.ResourceType, URL: resource.URL,
			ProjectID: &project.ID, IsActive: &active,
		})
		if err != nil {
			return nil, fmt.Errorf("import project resource %q: %w", resource.Title, err)
		}
		if resource.Summary != nil && strings.TrimSpace(*resource.Summary) != "" {
			if err := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).Where("id = ?", created.ID).
				Update("summary", *resource.Summary).Error; err != nil {
				return nil, fmt.Errorf("restore imported resource summary: %w", err)
			}
		}
	}
	return s.projects.Get(ctx, project.ID)
}

func (s *ProjectPortabilityService) Duplicate(ctx context.Context, projectID uint64, in ProjectDuplicateInput) (*ProjectView, error) {
	bundle, err := s.Export(ctx, projectID)
	if err != nil {
		return nil, err
	}
	bundle.Project.Name = strings.TrimSpace(in.Name)
	bundle.Project.Code = trimOptional(in.Code)
	return s.Import(ctx, ProjectImportInput{Bundle: *bundle})
}

func importedProjectInput(in ProjectImportInput) (ProjectInput, error) {
	if in.Bundle.SchemaVersion != ProjectBundleSchemaVersion {
		return ProjectInput{}, invalid(fmt.Errorf("unsupported project bundle schema_version %d", in.Bundle.SchemaVersion))
	}
	project := in.Bundle.Project
	if in.Name != nil {
		project.Name = strings.TrimSpace(*in.Name)
	}
	if in.Code != nil {
		project.Code = trimOptional(in.Code)
	}
	if err := project.validate(); err != nil {
		return ProjectInput{}, invalid(err)
	}
	for _, resource := range in.Bundle.Resources {
		input := ResourceInput{Title: resource.Title, ResourceType: resource.ResourceType, URL: resource.URL}
		if err := input.validate(); err != nil {
			return ProjectInput{}, invalid(fmt.Errorf("invalid resource %q: %w", resource.Title, err))
		}
	}
	return project, nil
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *ProjectPortabilityService) ResolveRepositories(ctx context.Context, projectID uint64) ([]RepositoryBinding, error) {
	if _, err := s.projects.Get(ctx, projectID); err != nil {
		return nil, err
	}
	var resources []domain.ManagedResource
	if err := s.db.WithContext(ctx).
		Where("project_id = ? AND resource_type = ? AND is_active = ?", projectID, "repo", true).
		Order("id").Find(&resources).Error; err != nil {
		return nil, fmt.Errorf("list project repositories: %w", err)
	}
	checkouts, err := discoverGitCheckouts(ctx, s.repoRoot)
	if err != nil {
		return nil, err
	}
	out := make([]RepositoryBinding, 0, len(resources))
	for _, resource := range resources {
		binding := RepositoryBinding{ResourceID: resource.ID, Title: resource.Title, Status: "unmatched"}
		if resource.URL != nil {
			binding.RemoteURL = *resource.URL
		}
		matches := checkouts[normalizeRemote(binding.RemoteURL)]
		switch len(matches) {
		case 1:
			path := matches[0]
			binding.LocalPath, binding.Status = &path, "matched"
			if err := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).
				Where("id = ?", resource.ID).Update("local_path", path).Error; err != nil {
				return nil, fmt.Errorf("bind repository resource id=%d: %w", resource.ID, err)
			}
		case 0:
			if err := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).
				Where("id = ?", resource.ID).Update("local_path", nil).Error; err != nil {
				return nil, fmt.Errorf("clear repository binding id=%d: %w", resource.ID, err)
			}
		default:
			binding.Status = "ambiguous"
			if err := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).
				Where("id = ?", resource.ID).Update("local_path", nil).Error; err != nil {
				return nil, fmt.Errorf("clear ambiguous repository binding id=%d: %w", resource.ID, err)
			}
		}
		out = append(out, binding)
	}
	return out, nil
}

func discoverGitCheckouts(ctx context.Context, root string) (map[string][]string, error) {
	found := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		depth := 0
		if relative != "." {
			depth = len(strings.Split(relative, string(os.PathSeparator)))
		}
		if depth > 2 {
			return filepath.SkipDir
		}
		if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
			return nil
		}
		command := exec.CommandContext(ctx, "git", "-C", path, "config", "--get", "remote.origin.url")
		output, err := command.Output()
		if err == nil {
			remote := normalizeRemote(string(output))
			if remote != "" {
				found[remote] = append(found[remote], path)
			}
		}
		return filepath.SkipDir
	})
	if err != nil {
		return nil, fmt.Errorf("scan repository root %q: %w", root, err)
	}
	return found, nil
}

func normalizeRemote(value string) string {
	raw := strings.TrimSpace(strings.TrimSuffix(value, "/"))
	raw = strings.TrimSuffix(raw, ".git")
	if strings.HasPrefix(raw, "git@") {
		raw = strings.TrimPrefix(raw, "git@")
		raw = strings.Replace(raw, ":", "/", 1)
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		raw = parsed.Hostname() + parsed.Path
	}
	return strings.ToLower(strings.Trim(raw, "/"))
}
