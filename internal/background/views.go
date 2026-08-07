package background

import (
	"encoding/json"
	"time"

	"jarvis/internal/domain"

	"jarvis/internal/datatypes"
)

// The API contract is snake_case (see web/src/types.ts). The domain models only
// carry GORM tags, so this package projects them into view structs with explicit
// JSON tags rather than leaking Go field names to the frontend.

// ProjectView is the API representation of a Project.
type ProjectView struct {
	ID           uint64          `json:"id"`
	Code         *string         `json:"code"`
	Name         string          `json:"name"`
	Role         string          `json:"role"`
	Status       string          `json:"status"`
	Priority     uint8           `json:"priority"`
	Description  *string         `json:"description"`
	Repos        json.RawMessage `json:"repos"`
	TechStack    json.RawMessage `json:"tech_stack"`
	KeyDecisions json.RawMessage `json:"key_decisions"`
	Timeline     json.RawMessage `json:"timeline"`
	Notes        *string         `json:"notes"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// KeyMatterView is the API representation of a KeyMatter.
type KeyMatterView struct {
	ID             uint64       `json:"id"`
	Title          string       `json:"title"`
	Status         string       `json:"status"`
	Summary        *string      `json:"summary"`
	ProjectID      *uint64      `json:"project_id"`
	DueAt          *time.Time   `json:"due_at"`
	ClosedAt       *time.Time   `json:"closed_at"`
	LastProgressAt *time.Time   `json:"last_progress_at"`
	LastActiveAt   time.Time    `json:"last_active_at"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	Project        *ProjectView `json:"project"`
}

// PersonView is the API representation of a Person.
type PersonView struct {
	ID             uint64    `json:"id"`
	OpenID         string    `json:"open_id"`
	UnionID        *string   `json:"union_id"`
	FeishuUserID   *string   `json:"feishu_user_id"`
	Name           string    `json:"name"`
	EnName         *string   `json:"en_name"`
	AvatarURL      *string   `json:"avatar_url"`
	Department     *string   `json:"department"`
	Title          *string   `json:"title"`
	Role           string    `json:"role"`
	PriorityWeight float64   `json:"priority_weight"`
	Relation       *string   `json:"relation"`
	CommStyle      *string   `json:"comm_style"`
	P2PChatID      *string   `json:"p2p_chat_id"`
	Notes          *string   `json:"notes"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// GroupView is the API representation of a Group, including its resolved project.
type GroupView struct {
	ID              uint64       `json:"id"`
	ChatID          string       `json:"chat_id"`
	ChatMode        string       `json:"chat_mode"`
	Name            *string      `json:"name"`
	Description     *string      `json:"description"`
	BackgroundNote  *string      `json:"background_note"`
	OwnerOpenID     *string      `json:"owner_open_id"`
	External        bool         `json:"external"`
	TenantKey       *string      `json:"tenant_key"`
	ProjectID       *uint64      `json:"project_id"`
	RelatedGroup    bool         `json:"related_group"`
	Tier            string       `json:"tier"`
	Pinned          bool         `json:"pinned"`
	IncludeInMemory bool         `json:"include_in_memory"`
	IsKeyGroup      bool         `json:"is_key_group"`
	LastActiveAt    *int64       `json:"last_active_at"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	Project         *ProjectView `json:"project"`
	// Read-only observability fields populated from chat_checkpoint / message.
	LastScanAt     *time.Time `json:"last_scan_at"`
	LastScanStatus *string    `json:"last_scan_status"`
	MessageCount   int64      `json:"message_count"`
}

func rawJSON(value datatypes.JSON) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return json.RawMessage(value)
}

func toProjectView(p *domain.Project) ProjectView {
	return ProjectView{
		ID: p.ID, Code: p.Code, Name: p.Name, Role: p.Role, Status: p.Status,
		Priority: p.Priority, Description: p.Description,
		Repos: rawJSON(p.Repos), TechStack: rawJSON(p.TechStack),
		KeyDecisions: rawJSON(p.KeyDecisions), Timeline: rawJSON(p.Timeline),
		Notes:     p.Notes,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func toProjectViews(items []domain.Project) []ProjectView {
	views := make([]ProjectView, len(items))
	for i := range items {
		views[i] = toProjectView(&items[i])
	}
	return views
}

func toKeyMatterView(matter *domain.KeyMatter) KeyMatterView {
	view := KeyMatterView{
		ID: matter.ID, Title: matter.Title, Status: matter.Status, Summary: matter.Summary,
		ProjectID: matter.ProjectID, DueAt: matter.DueAt, ClosedAt: matter.ClosedAt,
		LastProgressAt: matter.LastProgressAt, LastActiveAt: matter.LastActiveAt,
		CreatedAt: matter.CreatedAt, UpdatedAt: matter.UpdatedAt,
	}
	if matter.Project != nil {
		projectView := toProjectView(matter.Project)
		view.Project = &projectView
	}
	return view
}

func toKeyMatterViews(items []domain.KeyMatter) []KeyMatterView {
	views := make([]KeyMatterView, len(items))
	for i := range items {
		views[i] = toKeyMatterView(&items[i])
	}
	return views
}

func toPersonView(p *domain.Person) PersonView {
	return PersonView{
		ID: p.ID, OpenID: p.OpenID, UnionID: p.UnionID, FeishuUserID: p.FeishuUserID,
		Name: p.Name, EnName: p.EnName, AvatarURL: p.AvatarURL, Department: p.Department,
		Title: p.Title, Role: p.Role, PriorityWeight: p.PriorityWeight, Relation: p.Relation,
		CommStyle: p.CommStyle, P2PChatID: p.P2PChatID, Notes: p.Notes, IsActive: p.IsActive,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func toPersonViews(items []domain.Person) []PersonView {
	views := make([]PersonView, len(items))
	for i := range items {
		views[i] = toPersonView(&items[i])
	}
	return views
}

func toGroupView(g *domain.Group) GroupView {
	view := GroupView{
		ID: g.ID, ChatID: g.ChatID, ChatMode: g.ChatMode, Name: g.Name,
		Description: g.Description, BackgroundNote: g.BackgroundNote, OwnerOpenID: g.OwnerOpenID, External: g.External,
		TenantKey: g.TenantKey, ProjectID: g.ProjectID, RelatedGroup: g.RelatedGroup,
		Tier: g.Tier, Pinned: g.Pinned, IncludeInMemory: g.IncludeInMemory,
		IsKeyGroup: g.IsKeyGroup, LastActiveAt: g.LastActiveAt,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
	if g.Project != nil {
		projectView := toProjectView(g.Project)
		view.Project = &projectView
	}
	return view
}

func toGroupViews(items []domain.Group) []GroupView {
	views := make([]GroupView, len(items))
	for i := range items {
		views[i] = toGroupView(&items[i])
	}
	return views
}
