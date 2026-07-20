// Package contextsnap defines the canonical background snapshot that M3 freezes
// onto a Todo at extraction time and that M4/M5 replay unchanged.
//
// Per docs/design-context-pipeline.md the context is assembled/inferred exactly
// once in M3, persisted into Todo.context_snapshot, and reused for the whole
// M3→M4→M5 chain. Owning the struct here (instead of in decide/) prevents the
// two ends from drifting into incompatible shapes.
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
	// Conversation is the surrounding chat context (several rounds around the
	// cited Messages) so M4/M5 can read the fuller thread, not just the single
	// evidence message. Messages stays the precise cited evidence; Conversation
	// is broader background.
	Conversation []Message        `json:"conversation,omitempty"`
	Memories     []map[string]any `json:"memories"`
	// Supplements are human clarifications added after extraction (from a
	// need_info or need_decision Todo). They are appended (never replaced) and
	// replayed to M4 codex on re-evaluation so the decision maker sees the extra
	// context/intent the extractor lacked.
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
	Background   *string `json:"background"`
	Preferences  *string `json:"preferences"`
	LeaderOpenID *string `json:"leader_open_id"`
	LeaderName   *string `json:"leader_name"`
}

// Project carries the inferred/bound project including repos (so M5 can locate
// the working directory) and the key decisions that frame the work.
type Project struct {
	ID           uint64          `json:"id"`
	Code         *string         `json:"code"`
	Name         string          `json:"name"`
	Role         string          `json:"role"`
	Description  *string         `json:"description"`
	Repos        json.RawMessage `json:"repos"`
	KeyDecisions json.RawMessage `json:"key_decisions"`
}

// Group is the originating Feishu conversation, including its announcement
// (description) which is often the strongest signal for project attribution.
type Group struct {
	ID          uint64  `json:"id"`
	ChatID      string  `json:"chat_id"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// Assigner is who handed the Todo over (leader/colleague), with the relation to
// the principal so executors understand priority and tone.
type Assigner struct {
	OpenID   string  `json:"open_id"`
	Name     *string `json:"name"`
	Role     *string `json:"role"`
	Title    *string `json:"title"`
	Relation *string `json:"relation"`
}

// Message is one piece of source evidence, copied verbatim at capture time.
type Message struct {
	MessageID    string `json:"message_id"`
	ChatID       string `json:"chat_id"`
	SenderOpenID string `json:"sender_open_id"`
	SenderName   string `json:"sender_name"`
	Content      string `json:"content"`
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
