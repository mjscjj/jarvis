package background

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// managedResourceTypes mirrors the enum in the DDL so bad input fails fast
// before it reaches MySQL.
var managedResourceTypes = map[string]struct{}{
	"doc": {}, "link": {}, "repo": {}, "note": {}, "other": {},
}

// ResourceInput is the create/update payload for a manually curated resource.
// A resource may be linked to at most one Person and one Project, and may be
// flagged as belonging to the principal ("me").
type ResourceInput struct {
	Title         string  `json:"title"`
	ResourceType  string  `json:"resource_type"`
	URL           *string `json:"url"`
	Description   *string `json:"description"`
	PersonID      *uint64 `json:"person_id"`
	ProjectID     *uint64 `json:"project_id"`
	LinkPrincipal bool    `json:"link_principal"`
	IsActive      *bool   `json:"is_active"`
}

func (in *ResourceInput) validate() error {
	if strings.TrimSpace(in.Title) == "" {
		return fmt.Errorf("resource title must not be blank")
	}
	if _, ok := managedResourceTypes[in.ResourceType]; !ok {
		return fmt.Errorf("resource_type %q is invalid", in.ResourceType)
	}
	if in.URL != nil && strings.TrimSpace(*in.URL) == "" {
		return fmt.Errorf("resource url must not be blank when provided")
	}
	if in.PersonID != nil && *in.PersonID == 0 {
		return fmt.Errorf("resource person_id must be positive when provided")
	}
	if in.ProjectID != nil && *in.ProjectID == 0 {
		return fmt.Errorf("resource project_id must be positive when provided")
	}
	return nil
}

// ResourceView is the API projection of a managed resource. It carries the
// linked person/project names so the list can render them without a second call.
type ResourceView struct {
	ID            uint64  `json:"id"`
	Title         string  `json:"title"`
	ResourceType  string  `json:"resource_type"`
	URL           *string `json:"url"`
	Description   *string `json:"description"`
	PersonID      *uint64 `json:"person_id"`
	PersonName    *string `json:"person_name"`
	ProjectID     *uint64 `json:"project_id"`
	ProjectName   *string `json:"project_name"`
	LinkPrincipal bool    `json:"link_principal"`
	IsActive      bool    `json:"is_active"`
}

// ResourceList is the paginated response for managed resources.
type ResourceList struct {
	Items    []ResourceView `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// ResourceFilter narrows a list to resources linked to a specific person,
// project, or the principal. Empty values leave that dimension unconstrained.
type ResourceFilter struct {
	ListFilter
	PersonID      *uint64
	ProjectID     *uint64
	PrincipalOnly bool
	ActiveOnly    bool
}

// ResourceService is the authoritative CRUD owner of the managed_resource table.
type ResourceService struct {
	db *gorm.DB
}

func NewResourceService(db *gorm.DB) (*ResourceService, error) {
	if db == nil {
		return nil, fmt.Errorf("resource service db is nil")
	}
	return &ResourceService{db: db}, nil
}

func (s *ResourceService) Create(ctx context.Context, in ResourceInput) (*ResourceView, error) {
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := s.ensureLinksExist(ctx, in.PersonID, in.ProjectID); err != nil {
		return nil, err
	}
	resource := domain.ManagedResource{
		Title: in.Title, ResourceType: in.ResourceType, URL: in.URL, Description: in.Description,
		PersonID: in.PersonID, ProjectID: in.ProjectID, LinkPrincipal: in.LinkPrincipal,
		IsActive: boolOrDefault(in.IsActive, true),
	}
	if err := s.db.WithContext(ctx).Create(&resource).Error; err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}
	return s.Get(ctx, resource.ID)
}

func (s *ResourceService) Get(ctx context.Context, id uint64) (*ResourceView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("resource id must be positive"))
	}
	var resource domain.ManagedResource
	err := s.db.WithContext(ctx).Preload("Person").Preload("Project").
		Where("id = ?", id).Take(&resource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get resource id=%d: %w", id, err)
	}
	view := toResourceView(&resource)
	return &view, nil
}

func (s *ResourceService) Update(ctx context.Context, id uint64, in ResourceInput) (*ResourceView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("resource id must be positive"))
	}
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := s.ensureLinksExist(ctx, in.PersonID, in.ProjectID); err != nil {
		return nil, err
	}
	// Explicit column list so an update never silently touches audit columns and
	// always overwrites the optional links to NULL when the caller omits them.
	updates := map[string]any{
		"title": in.Title, "resource_type": in.ResourceType, "url": in.URL,
		"description": in.Description, "person_id": in.PersonID, "project_id": in.ProjectID,
		"link_principal": in.LinkPrincipal, "is_active": boolOrDefault(in.IsActive, true),
	}
	result := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update resource id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *ResourceService) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return invalid(fmt.Errorf("resource id must be positive"))
	}
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.ManagedResource{})
	if result.Error != nil {
		return fmt.Errorf("delete resource id=%d: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *ResourceService) List(ctx context.Context, filter ResourceFilter) (*ResourceList, error) {
	if err := filter.validate(); err != nil {
		return nil, invalid(err)
	}
	query := s.db.WithContext(ctx).Model(&domain.ManagedResource{})
	query = applyResourceFilter(query, filter)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count resources: %w", err)
	}
	items := make([]domain.ManagedResource, 0, filter.PageSize)
	if total > 0 {
		listQuery := applyResourceFilter(s.db.WithContext(ctx).Model(&domain.ManagedResource{}), filter)
		if err := listQuery.
			Preload("Person").Preload("Project").
			Order("id DESC").
			Limit(filter.PageSize).
			Offset(filter.offset()).
			Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list resources: %w", err)
		}
	}
	return &ResourceList{Items: toResourceViews(items), Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

// ensureLinksExist validates that any referenced person/project actually exists,
// so a resource can never dangle against a non-existent link (fail-fast).
func (s *ResourceService) ensureLinksExist(ctx context.Context, personID, projectID *uint64) error {
	if personID != nil {
		var count int64
		if err := s.db.WithContext(ctx).Model(&domain.Person{}).Where("id = ?", *personID).Count(&count).Error; err != nil {
			return fmt.Errorf("verify resource person_id=%d: %w", *personID, err)
		}
		if count == 0 {
			return invalid(fmt.Errorf("resource person_id=%d does not exist", *personID))
		}
	}
	if projectID != nil {
		var count int64
		if err := s.db.WithContext(ctx).Model(&domain.Project{}).Where("id = ?", *projectID).Count(&count).Error; err != nil {
			return fmt.Errorf("verify resource project_id=%d: %w", *projectID, err)
		}
		if count == 0 {
			return invalid(fmt.Errorf("resource project_id=%d does not exist", *projectID))
		}
	}
	return nil
}

func applyResourceFilter(query *gorm.DB, filter ResourceFilter) *gorm.DB {
	if filter.PersonID != nil {
		query = query.Where("person_id = ?", *filter.PersonID)
	}
	if filter.ProjectID != nil {
		query = query.Where("project_id = ?", *filter.ProjectID)
	}
	if filter.PrincipalOnly {
		query = query.Where("link_principal = ?", true)
	}
	if filter.ActiveOnly {
		query = query.Where("is_active = ?", true)
	}
	return query
}

func toResourceView(resource *domain.ManagedResource) ResourceView {
	view := ResourceView{
		ID: resource.ID, Title: resource.Title, ResourceType: resource.ResourceType,
		URL: resource.URL, Description: resource.Description,
		PersonID: resource.PersonID, ProjectID: resource.ProjectID,
		LinkPrincipal: resource.LinkPrincipal, IsActive: resource.IsActive,
	}
	if resource.Person != nil {
		name := resource.Person.Name
		view.PersonName = &name
	}
	if resource.Project != nil {
		name := resource.Project.Name
		view.ProjectName = &name
	}
	return view
}

func toResourceViews(resources []domain.ManagedResource) []ResourceView {
	views := make([]ResourceView, len(resources))
	for i := range resources {
		views[i] = toResourceView(&resources[i])
	}
	return views
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
