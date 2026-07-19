package contextsnap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Resolution records how M3 decided which project (and repo hint) a Todo belongs
// to, so both the user and the executor can see the reasoning. It is persisted
// into Todo.resolution alongside the snapshot.
type Resolution struct {
	// Method is how project_id was determined.
	Method      string  `json:"method"` // group_bound | codex_cli | project_hint | unresolved
	ProjectID   *uint64 `json:"project_id"`
	ProjectName *string `json:"project_name"`
	ReposHint   *string `json:"repos_hint"`
	Confidence  float64 `json:"confidence"`
	Basis       string  `json:"basis"`
}

// Resolution method constants.
const (
	MethodGroupBound  = "group_bound"  // inherited from the group's bound project (highest priority)
	MethodProjectHint = "project_hint" // matched the model's project_hint against project.code/name
	MethodCodexCLI    = "codex_cli"    // codex self-queried lark-cli/git to attribute the project
	MethodUnresolved  = "unresolved"   // no project could be determined
)

// Encode serializes the resolution, validating the method and confidence range.
func (r Resolution) Encode() (json.RawMessage, error) {
	switch r.Method {
	case MethodGroupBound, MethodProjectHint, MethodCodexCLI, MethodUnresolved:
	default:
		return nil, fmt.Errorf("resolution method %q is invalid", r.Method)
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		return nil, fmt.Errorf("resolution confidence %v is outside [0,1]", r.Confidence)
	}
	if strings.TrimSpace(r.Basis) == "" {
		return nil, fmt.Errorf("resolution basis is empty")
	}
	if r.Method == MethodUnresolved && r.ProjectID != nil {
		return nil, fmt.Errorf("resolution method unresolved must not carry project_id")
	}
	if r.Method != MethodUnresolved && r.ProjectID == nil {
		return nil, fmt.Errorf("resolution method %q requires project_id", r.Method)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode resolution: %w", err)
	}
	return json.RawMessage(encoded), nil
}
