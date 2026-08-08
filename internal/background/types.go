// Package background owns M1: the manually maintained Project / KeyMatter / Person / Group
// backgrounds that give every downstream module (extraction, execution) its
// context. It is the authoritative writer for Project and Person, and the
// authoritative writer for the *human-curated* subset of Group columns only —
// capture (M2) still owns the discovery columns (chat_id/name/tier/...), so this
// package never creates or deletes a Group and only patches background fields.
package background

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// projectRoles / projectStatuses / personRoles are the application-level values.
// Keeping them here lets us fail fast on bad input before it reaches SQLite.
var projectRoles = map[string]struct{}{"owner": {}, "participant": {}}

var projectStatuses = map[string]struct{}{
	"planning": {}, "active": {}, "paused": {}, "archived": {}, "done": {},
}

var personRoles = map[string]struct{}{
	"leader": {}, "key": {}, "colleague": {}, "other": {},
}

const maxPageSize = 100

// ListFilter is the shared pagination contract for every list endpoint.
type ListFilter struct {
	Page     int
	PageSize int
}

func (f ListFilter) validate() error {
	if f.Page < 1 {
		return fmt.Errorf("page must be >= 1")
	}
	if f.PageSize < 1 || f.PageSize > maxPageSize {
		return fmt.Errorf("page_size must be between 1 and %d", maxPageSize)
	}
	return nil
}

func (f ListFilter) offset() int { return (f.Page - 1) * f.PageSize }

// ProjectInput is the create/update payload for a Project. JSON columns accept
// raw JSON so the caller keeps full control of their shape; nil means "leave
// unset" on create and is stored as SQL NULL.
type ProjectInput struct {
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
}

func (in *ProjectInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("project name must not be blank")
	}
	if _, ok := projectRoles[in.Role]; !ok {
		return fmt.Errorf("project role %q is invalid", in.Role)
	}
	if _, ok := projectStatuses[in.Status]; !ok {
		return fmt.Errorf("project status %q is invalid", in.Status)
	}
	if in.Priority < 1 || in.Priority > 5 {
		return fmt.Errorf("project priority must be between 1 and 5")
	}
	if in.Code != nil && strings.TrimSpace(*in.Code) == "" {
		return fmt.Errorf("project code must not be blank when provided")
	}
	for name, raw := range map[string]json.RawMessage{
		"repos": in.Repos, "tech_stack": in.TechStack,
		"key_decisions": in.KeyDecisions, "timeline": in.Timeline,
	} {
		if err := validateOptionalJSON(name, raw); err != nil {
			return err
		}
	}
	return nil
}

// KeyMatterInput is the complete editable payload for a KeyMatter.
type KeyMatterInput struct {
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Summary   *string    `json:"summary"`
	ProjectID *uint64    `json:"project_id"`
	DueAt     *time.Time `json:"due_at"`
}

func (in *KeyMatterInput) validate() error {
	if strings.TrimSpace(in.Title) == "" {
		return fmt.Errorf("key matter title must not be blank")
	}
	return nil
}

// PersonUpdateInput contains only the human-editable Person fields. Identity
// fields belong to the existing Person record and must not be rewritten by an
// edit form.
type PersonUpdateInput struct {
	Name           string  `json:"name"`
	Department     *string `json:"department"`
	Title          *string `json:"title"`
	Role           string  `json:"role"`
	PriorityWeight float64 `json:"priority_weight"`
	Relation       *string `json:"relation"`
	CommStyle      *string `json:"comm_style"`
	Notes          *string `json:"notes"`
	IsActive       *bool   `json:"is_active"`
}

func (in *PersonUpdateInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("person name must not be blank")
	}
	if _, ok := personRoles[in.Role]; !ok {
		return fmt.Errorf("person role %q is invalid", in.Role)
	}
	if in.PriorityWeight < 0 || in.PriorityWeight > 1 {
		return fmt.Errorf("person priority_weight must be between 0 and 1")
	}
	return nil
}

// PersonCreateInput includes the external identity required when a Person is
// first bound. Later updates use PersonUpdateInput and preserve these fields.
type PersonCreateInput struct {
	PersonUpdateInput
	OpenID       string  `json:"open_id"`
	UnionID      *string `json:"union_id"`
	FeishuUserID *string `json:"feishu_user_id"`
	EnName       *string `json:"en_name"`
	AvatarURL    *string `json:"avatar_url"`
	P2PChatID    *string `json:"p2p_chat_id"`
}

func (in *PersonCreateInput) validate() error {
	if strings.TrimSpace(in.OpenID) == "" {
		return fmt.Errorf("person open_id must not be blank")
	}
	return in.PersonUpdateInput.validate()
}

// GroupBackgroundInput is the human-curated subset of Group. It deliberately
// omits every discovery column owned by capture (chat_id/name/description/tier/...).
type GroupBackgroundInput struct {
	BackgroundNote  *string `json:"background_note"`
	ProjectID       *uint64 `json:"project_id"`
	RelatedGroup    bool    `json:"related_group"`
	Pinned          bool    `json:"pinned"`
	IncludeInMemory bool    `json:"include_in_memory"`
	IsKeyGroup      bool    `json:"is_key_group"`
}

func validateOptionalJSON(field string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if !json.Valid(raw) {
		return fmt.Errorf("%s must be valid JSON", field)
	}
	return nil
}
