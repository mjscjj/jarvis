package contextsnap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// AssembleOptions describes the source-specific facts that are combined with
// Jarvis' authoritative principal/project background for a manual or scheduled
// Task. RequestContext is preserved as data; it never replaces common context.
type AssembleOptions struct {
	ProjectID      *uint64
	RequestContext json.RawMessage
}

// Assembler builds the canonical background for Task sources that do not pass
// through M3. M3 has richer conversation-local data and builds the same wire
// shape directly from its already loaded batch.
type Assembler struct {
	db              *gorm.DB
	principalOpenID string
	now             func() time.Time
}

func NewAssembler(db *gorm.DB, principalOpenID string) (*Assembler, error) {
	if db == nil {
		return nil, fmt.Errorf("context snapshot assembler db is nil")
	}
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return nil, fmt.Errorf("context snapshot assembler principal open_id is empty")
	}
	return &Assembler{db: db, principalOpenID: principalOpenID, now: time.Now}, nil
}

func (a *Assembler) Assemble(ctx context.Context, options AssembleOptions) (json.RawMessage, error) {
	requestContext, hintedProjectID, err := normalizeRequestContext(options.RequestContext)
	if err != nil {
		return nil, err
	}
	projectID := options.ProjectID
	if projectID == nil {
		projectID = hintedProjectID
	}
	if projectID != nil && *projectID == 0 {
		return nil, fmt.Errorf("assemble context snapshot: project_id must be positive")
	}

	principal, err := a.loadPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	project, err := a.loadProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	otherProjects, err := a.loadOtherProjects(ctx, projectID)
	if err != nil {
		return nil, err
	}
	managedResources, err := a.loadManagedResources(ctx, projectID)
	if err != nil {
		return nil, err
	}
	projectEvents, err := a.loadProjectEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}

	snapshot := Snapshot{
		SnapshotVersion:  SnapshotVersion,
		CapturedAt:       a.now().UTC().Format(time.RFC3339),
		Principal:        principal,
		Project:          project,
		OtherProjects:    otherProjects,
		ManagedResources: managedResources,
		ProjectEvents:    projectEvents,
		Memories:         make([]map[string]any, 0),
		RequestContext:   requestContext,
	}
	raw, err := snapshot.Encode()
	if err != nil {
		return nil, fmt.Errorf("assemble context snapshot: %w", err)
	}
	return raw, nil
}

func (a *Assembler) loadPrincipal(ctx context.Context) (*Principal, error) {
	var row domain.PrincipalProfile
	err := a.db.WithContext(ctx).Where("open_id = ?", a.principalOpenID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("assemble context snapshot: principal profile open_id=%s is not configured", a.principalOpenID)
	}
	if err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load principal profile: %w", err)
	}
	return &Principal{
		OpenID: row.OpenID, Name: row.Name, Department: copyString(row.Department),
		Title: copyString(row.Title), Background: copyString(row.Background),
		Preferences: copyString(row.Preferences), LeaderOpenID: copyString(row.LeaderOpenID),
		LeaderName: copyString(row.LeaderName),
	}, nil
}

func (a *Assembler) loadProject(ctx context.Context, projectID *uint64) (*Project, error) {
	if projectID == nil {
		return nil, nil
	}
	var row domain.Project
	err := a.db.WithContext(ctx).Where("id = ?", *projectID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("assemble context snapshot: project_id=%d does not exist", *projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load project_id=%d: %w", *projectID, err)
	}
	return projectFromDomain(&row), nil
}

func (a *Assembler) loadOtherProjects(ctx context.Context, selectedID *uint64) ([]ProjectBrief, error) {
	var rows []domain.Project
	query := a.db.WithContext(ctx).Where("status <> ?", "archived")
	if selectedID != nil {
		query = query.Where("id <> ?", *selectedID)
	}
	if err := query.Order("priority ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load project catalog: %w", err)
	}
	result := make([]ProjectBrief, len(rows))
	for i := range rows {
		result[i] = ProjectBrief{
			ID: rows[i].ID, Code: copyString(rows[i].Code), Name: rows[i].Name,
			Role: rows[i].Role, Status: rows[i].Status, Priority: rows[i].Priority,
			Description: copyString(rows[i].Description),
		}
	}
	return result, nil
}

func (a *Assembler) loadManagedResources(ctx context.Context, projectID *uint64) ([]ManagedResource, error) {
	query := a.db.WithContext(ctx).Where("is_active = ?", true)
	if projectID == nil {
		query = query.Where("link_principal = ?", true)
	} else {
		query = query.Where("link_principal = ? OR project_id = ?", true, *projectID)
	}
	var rows []domain.ManagedResource
	if err := query.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load managed resources: %w", err)
	}
	result := make([]ManagedResource, len(rows))
	for i := range rows {
		result[i] = ManagedResource{
			ID: rows[i].ID, Title: rows[i].Title, ResourceType: rows[i].ResourceType,
			URL: copyString(rows[i].URL), Description: copyString(rows[i].Description),
			ProjectID: copyUint64(rows[i].ProjectID), LinkPrincipal: rows[i].LinkPrincipal,
		}
	}
	return result, nil
}

func (a *Assembler) loadProjectEvents(ctx context.Context, projectID *uint64) ([]ProjectEvent, error) {
	if projectID == nil {
		return nil, nil
	}
	var rows []domain.ProjectEvent
	if err := a.db.WithContext(ctx).Where("project_id = ?", *projectID).
		Order("occurred_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("assemble context snapshot: load project events project_id=%d: %w", *projectID, err)
	}
	result := make([]ProjectEvent, len(rows))
	for i := range rows {
		result[i] = ProjectEvent{
			ID: rows[i].ID, Description: rows[i].Description,
			OccurredAt: rows[i].OccurredAt.UTC().Format(time.RFC3339),
		}
	}
	return result, nil
}

func projectFromDomain(row *domain.Project) *Project {
	if row == nil {
		return nil
	}
	return &Project{
		ID: row.ID, Code: copyString(row.Code), Name: row.Name, Role: row.Role,
		Status: row.Status, Priority: row.Priority, Description: copyString(row.Description),
		Repos: rawJSONOrNull(row.Repos), TechStack: rawJSONOrNull(row.TechStack),
		KeyDecisions: rawJSONOrNull(row.KeyDecisions), Timeline: rawJSONOrNull(row.Timeline),
		Notes: copyString(row.Notes),
	}
}

func normalizeRequestContext(raw json.RawMessage) (json.RawMessage, *uint64, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, nil, fmt.Errorf("assemble context snapshot: request context must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("assemble context snapshot: request context has trailing data")
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, nil, fmt.Errorf("assemble context snapshot: encode request context: %w", err)
	}
	projectID, err := projectHint(object)
	if err != nil {
		return nil, nil, err
	}
	return encoded, projectID, nil
}

func projectHint(object map[string]any) (*uint64, error) {
	value, ok := object["project_id"]
	if !ok {
		if project, projectOK := object["project"].(map[string]any); projectOK {
			value, ok = project["id"]
		}
	}
	if !ok {
		return nil, nil
	}
	number, ok := value.(json.Number)
	if !ok {
		return nil, fmt.Errorf("assemble context snapshot: request context project id must be an integer")
	}
	id, err := number.Int64()
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("assemble context snapshot: request context project id must be a positive integer")
	}
	result := uint64(id)
	return &result, nil
}

func rawJSONOrNull(raw []byte) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), raw...))
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
