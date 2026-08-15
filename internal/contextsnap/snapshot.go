// Package contextsnap defines the canonical background snapshot that M3 freezes
// onto a Todo at extraction time. It travels unchanged into Task.background and
// is handed to M5 whole.
//
// Per docs/design-context-pipeline.md the context is assembled/inferred exactly
// once in M3, persisted into Todo.context_snapshot, and reused for the whole
// M3→M5 chain. Owning the struct here prevents the two ends from drifting into
// incompatible shapes.
package contextsnap

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Snapshot is the frozen background for a single Todo. Every consumer decodes
// this exact shape; fields are pointers/slices so "absent" is explicit.
type Snapshot struct {
	// SnapshotVersion lets consumers fail-fast on an unexpected shape instead of
	// silently mis-reading an old snapshot.
	SnapshotVersion string `json:"snapshot_version"`
	CapturedAt      string `json:"captured_at"` // RFC3339 UTC

	Principal *Principal `json:"principal"`
	Project   *Project   `json:"project"`
	Group     *Group     `json:"group"`
	Assigner  *Assigner  `json:"assigner"`
	Messages  []Message  `json:"messages"`
	// Participants/resources/other_projects are part of the exact context M3
	// used to extract the clue.
	//
	// Open Todos and recent Tasks deliberately do not ride here. They are world
	// state, not evidence: a snapshot freezes what was true at creation time,
	// but "what else is already being worked on" is only useful as of the moment
	// someone acts on it. M5 loads that fresh on every run; see
	// internal/execute/currentworld.go.
	Participants  []Participant  `json:"participants,omitempty"`
	Resources     []Resource     `json:"resources,omitempty"`
	OtherProjects []ProjectBrief `json:"other_projects,omitempty"`
	// Conversation is the surrounding chat context (several rounds around the
	// cited Messages). Messages stays the precise cited evidence; Conversation
	// is the broader background around it.
	Conversation []Message        `json:"conversation,omitempty"`
	Memories     []map[string]any `json:"memories"`
	// ManagedResources is loaded by the common context assembler for
	// manual/scheduled tasks. M3 leaves it empty because its own captured
	// resources are frozen in Resources above.
	//
	// Fact detail deliberately does not ride here. A snapshot answers "what was
	// the world when this was created"; the entity Summary fields above already
	// carry that. Fact history is drilled into on demand with list-facts.
	ManagedResources []ManagedResource `json:"managed_resources,omitempty"`
	// RequestContext preserves caller-supplied manual/scheduled background
	// without allowing it to replace the authoritative common snapshot.
	RequestContext json.RawMessage `json:"request_context,omitempty"`
	// Supplements are human clarifications added after extraction. They are
	// appended rather than replacing the frozen background.
	Supplements []Supplement `json:"supplements,omitempty"`
}

// Supplement is one human clarification added to a Todo after extraction.
type Supplement struct {
	Note string `json:"note"`
	At   string `json:"at"` // RFC3339 UTC
}

// Principal is the decision-maker ("me"): who I am, what I own, who my leader is.
type Principal struct {
	OpenID       string  `json:"open_id"`
	Name         string  `json:"name"`
	Department   *string `json:"department"`
	Title        *string `json:"title"`
	Summary      *string `json:"summary,omitempty"`
	LeaderOpenID *string `json:"leader_open_id"`
	LeaderName   *string `json:"leader_name"`
}

// Project is the inferred/bound project.
type Project struct {
	ID       uint64  `json:"id"`
	Code     *string `json:"code"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Status   string  `json:"status,omitempty"`
	Priority uint8   `json:"priority,omitempty"`
	Summary  *string `json:"summary,omitempty"`
}

// Group is the originating Feishu conversation. Description is the captured
// announcement; Summary is Jarvis's long-term truth about the group.
type Group struct {
	ID          uint64  `json:"id"`
	ChatID      string  `json:"chat_id"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Summary     *string `json:"summary,omitempty"`
	IsKeyGroup  bool    `json:"is_key_group"`
	ProjectID   *uint64 `json:"project_id"`
}

// Assigner is who handed the Todo over (leader/colleague).
type Assigner struct {
	OpenID  string  `json:"open_id"`
	Name    *string `json:"name"`
	Role    *string `json:"role"`
	Title   *string `json:"title"`
	Summary *string `json:"summary,omitempty"`
}

// Participant freezes the people information M3 used to interpret tone,
// authority and implicit assignments.
type Participant struct {
	OpenID   string  `json:"open_id"`
	Name     *string `json:"name,omitempty"`
	Role     *string `json:"role,omitempty"`
	Title    *string `json:"title,omitempty"`
	IsLeader bool    `json:"is_leader"`
	Summary  *string `json:"summary,omitempty"`
}

// Resource is a captured attachment/document referenced by the conversation.
type Resource struct {
	ID            uint64  `json:"id"`
	ResourceType  string  `json:"resource_type"`
	FileKey       *string `json:"file_key,omitempty"`
	MinuteToken   *string `json:"minute_token,omitempty"`
	DocToken      *string `json:"doc_token,omitempty"`
	URL           *string `json:"url,omitempty"`
	Name          *string `json:"name,omitempty"`
	ExtractedText *string `json:"extracted_text,omitempty"`
}

type ProjectBrief struct {
	ID       uint64  `json:"id"`
	Code     *string `json:"code,omitempty"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Status   string  `json:"status,omitempty"`
	Priority uint8   `json:"priority,omitempty"`
}

type ManagedResource struct {
	ID            uint64  `json:"id"`
	Title         string  `json:"title"`
	ResourceType  string  `json:"resource_type"`
	URL           *string `json:"url,omitempty"`
	Summary       *string `json:"summary,omitempty"`
	ProjectID     *uint64 `json:"project_id,omitempty"`
	LinkPrincipal bool    `json:"link_principal"`
	LastActiveAt  string  `json:"last_active_at"`
}

// Message is one piece of source evidence, copied verbatim at capture time.
type Message struct {
	MessageID    string `json:"message_id"`
	ChatID       string `json:"chat_id"`
	SenderOpenID string `json:"sender_open_id"`
	SenderName   string `json:"sender_name"`
	Content      string `json:"content"`
	RootID       string `json:"root_id,omitempty"`
	ThreadID     string `json:"thread_id,omitempty"`
	CreateTime   int64  `json:"create_time"`
}

// SnapshotVersion is the current wire version. Bump when the shape changes so
// stale snapshots are rejected rather than mis-read.
const SnapshotVersion = "v1"

// Encode serializes the snapshot to canonical JSON, failing if it is empty of
// meaningful content (fail-fast: an empty snapshot must never reach the DB).
func (s Snapshot) Encode() (json.RawMessage, error) {
	if strings.TrimSpace(s.SnapshotVersion) == "" {
		return nil, fmt.Errorf("context snapshot version is empty")
	}
	if s.Principal == nil && s.Project == nil && s.Group == nil && len(s.Messages) == 0 {
		return nil, fmt.Errorf("context snapshot has no principal/project/group/messages")
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("encode context snapshot: %w", err)
	}
	return json.RawMessage(encoded), nil
}

// Decode parses a persisted snapshot, rejecting empty payloads and unknown
// versions so a mismatched shape surfaces instead of silently degrading.
func Decode(raw []byte) (*Snapshot, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("context snapshot is empty")
	}
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode context snapshot: %w", err)
	}
	if snapshot.SnapshotVersion != SnapshotVersion {
		return nil, fmt.Errorf("context snapshot version %q is unsupported, want %q", snapshot.SnapshotVersion, SnapshotVersion)
	}
	return &snapshot, nil
}
