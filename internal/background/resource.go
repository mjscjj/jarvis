package background

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// managedResourceTypes defines the application-level values so bad input fails
// fast before it reaches SQLite.
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
	ID            uint64    `json:"id"`
	Title         string    `json:"title"`
	ResourceType  string    `json:"resource_type"`
	URL           *string   `json:"url"`
	Description   *string   `json:"description"`
	PersonID      *uint64   `json:"person_id"`
	PersonName    *string   `json:"person_name"`
	ProjectID     *uint64   `json:"project_id"`
	ProjectName   *string   `json:"project_name"`
	LinkPrincipal bool      `json:"link_principal"`
	IsActive      bool      `json:"is_active"`
	LastActiveAt  time.Time `json:"last_active_at"`
}

// ResourceList is the paginated response for managed resources.
type ResourceList struct {
	Items       []ResourceView `json:"items"`
	Total       int64          `json:"total"`
	ActiveTotal int64          `json:"active_total"`
	Page        int            `json:"page"`
	PageSize    int            `json:"page_size"`
	MaxActive   int            `json:"max_active"`
}

const maxActiveManagedResources = 50

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
	db  *gorm.DB
	now func() time.Time
	mu  sync.Mutex
}

func NewResourceService(db *gorm.DB) (*ResourceService, error) {
	if db == nil {
		return nil, fmt.Errorf("resource service db is nil")
	}
	return &ResourceService{db: db, now: time.Now}, nil
}

func (s *ResourceService) Create(ctx context.Context, in ResourceInput) (*ResourceView, error) {
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := s.ensureLinksExist(ctx, in.PersonID, in.ProjectID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	requestedActive := boolOrDefault(in.IsActive, true)
	resource := domain.ManagedResource{
		Title: in.Title, ResourceType: in.ResourceType, URL: in.URL, Description: in.Description,
		PersonID: in.PersonID, ProjectID: in.ProjectID, LinkPrincipal: in.LinkPrincipal,
		IsActive: requestedActive, LastActiveAt: s.now().UTC(),
	}
	if requestedActive {
		if err := s.requireActiveCapacity(ctx); err != nil {
			return nil, err
		}
	}
	if err := s.db.WithContext(ctx).Create(&resource).Error; err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}
	// GORM applies the schema's default=true to a false zero value on create.
	// Persist an explicitly requested inactive state as the second fail-fast
	// write; the local database serializes writers and this project deliberately
	// does not wrap multi-step background writes in transactions.
	if !requestedActive {
		result := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).Where("id = ?", resource.ID).UpdateColumn("is_active", false)
		if result.Error != nil {
			return nil, fmt.Errorf("deactivate newly created resource id=%d: %w", resource.ID, result.Error)
		}
		if result.RowsAffected != 1 {
			return nil, fmt.Errorf("deactivate newly created resource id=%d affected %d rows", resource.ID, result.RowsAffected)
		}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	var before domain.ManagedResource
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&before).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load resource id=%d before update: %w", id, err)
	}
	nextActive := boolOrDefault(in.IsActive, true)
	if !before.IsActive && nextActive {
		if err := s.requireActiveCapacity(ctx); err != nil {
			return nil, err
		}
	}
	// Explicit column list so an update never silently touches audit columns and
	// always overwrites the optional links to NULL when the caller omits them.
	updates := map[string]any{
		"title": in.Title, "resource_type": in.ResourceType, "url": in.URL,
		"description": in.Description, "person_id": in.PersonID, "project_id": in.ProjectID,
		"link_principal": in.LinkPrincipal, "is_active": nextActive,
	}
	if !before.IsActive && nextActive {
		updates["last_active_at"] = s.now().UTC()
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

// Touch marks one enabled resource as freshly relevant without mutating its
// descriptive fields. Disabled resources must be re-enabled explicitly first.
func (s *ResourceService) Touch(ctx context.Context, id uint64) (*ResourceView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("resource id must be positive"))
	}
	var resource domain.ManagedResource
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&resource).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load resource id=%d before touch: %w", id, err)
	}
	if !resource.IsActive {
		return nil, invalid(fmt.Errorf("resource id=%d is inactive", id))
	}
	activeAt := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).Where("id = ? AND is_active = ?", id, true).
		UpdateColumn("last_active_at", activeAt)
	if result.Error != nil {
		return nil, fmt.Errorf("touch resource id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("touch resource id=%d affected %d rows", id, result.RowsAffected)
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
	var activeTotal int64
	if err := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).Where("is_active = ?", true).Count(&activeTotal).Error; err != nil {
		return nil, fmt.Errorf("count active resources: %w", err)
	}
	items := make([]domain.ManagedResource, 0, filter.PageSize)
	if total > 0 {
		listQuery := applyResourceFilter(s.db.WithContext(ctx).Model(&domain.ManagedResource{}), filter)
		if err := listQuery.
			Preload("Person").Preload("Project").
			Order("datetime(last_active_at) DESC, id DESC").
			Limit(filter.PageSize).
			Offset(filter.offset()).
			Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list resources: %w", err)
		}
	}
	return &ResourceList{
		Items: toResourceViews(items), Total: total, ActiveTotal: activeTotal,
		Page: filter.Page, PageSize: filter.PageSize, MaxActive: maxActiveManagedResources,
	}, nil
}

func (s *ResourceService) requireActiveCapacity(ctx context.Context) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&domain.ManagedResource{}).Where("is_active = ?", true).Count(&count).Error; err != nil {
		return fmt.Errorf("count active resources before activation: %w", err)
	}
	if count >= maxActiveManagedResources {
		return invalid(fmt.Errorf("active managed resource limit reached: %d", maxActiveManagedResources))
	}
	return nil
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
		LinkPrincipal: resource.LinkPrincipal, IsActive: resource.IsActive, LastActiveAt: resource.LastActiveAt,
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
