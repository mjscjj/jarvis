package extract

import (
	"context"
	"time"
)

// Prompt is the provider-independent input to the structured-output model.
type Prompt struct {
	System string
	User   string
}

type LoadOptions struct {
	BatchMessages   int
	ContextMessages int
	ContextWindow   time.Duration
	OpenTodoLimit   int
}

type GroupContext struct {
	ID          uint64
	ChatID      string
	Name        string
	Description string // group announcement; a strong signal for project attribution
	IsKeyGroup  bool
	ProjectID   *uint64
}

type ProjectContext struct {
	ID           uint64
	Code         string
	Name         string
	Role         string
	Status       string
	Priority     uint8
	Description  string
	Repos        []byte
	TechStack    []byte
	KeyDecisions []byte
	Timeline     []byte
	Notes        string
}

// OtherProjectContext is the concise projection of a project the group is NOT
// bound to. It gives the model a lightweight map of the principal's other work
// (so it can attribute a clue to the right area) without the full detail of the
// bound project.
type OtherProjectContext struct {
	ID          uint64
	Code        string
	Name        string
	Role        string
	Status      string
	Priority    uint8
	Description string
}

// PrincipalContext is the decision-maker ("me") background fed to the model so
// it knows who the principal is, what they own, and who their direct leader is —
// which is decisive for reading a leader's soft-worded assignment as a real
// action clue.
type PrincipalContext struct {
	OpenID       string
	Name         string
	Department   string
	Title        string
	Background   string
	Preferences  string
	LeaderOpenID string
	LeaderName   string
}

type MessageContext struct {
	DatabaseID   uint64
	MessageID    string
	ChatID       string
	SenderOpenID string
	SenderName   string
	SenderType   string
	Source       string
	MessageType  string
	Content      string
	RootID       string
	ThreadID     string
	CreateTime   int64
	IsNew        bool
	IsLeader     bool
	Extractable  bool
}

type ParticipantContext struct {
	OpenID    string
	Name      string
	Role      string
	Title     string
	IsLeader  bool
	Relation  string
	CommStyle string
}

type ResourceContext struct {
	ID            uint64
	ResourceType  string
	FileKey       string
	MinuteToken   string
	DocToken      string
	URL           string
	Name          string
	ExtractedText string
}

type OpenTodoContext struct {
	ID         uint64
	ActionType string
	Title      string
	Status     string
}

type ConversationUnit struct {
	Key          string
	Messages     []MessageContext
	Participants []ParticipantContext
	Resources    []ResourceContext
}

type ChatBatch struct {
	Group         GroupContext
	Project       *ProjectContext
	OtherProjects []OtherProjectContext
	Principal     *PrincipalContext
	OpenTodos     []OpenTodoContext
	Units         []ConversationUnit
	LastNew       MessageContext
}

type UnitExtraction struct {
	UnitKey    string
	Candidates []ResolvedCandidate
	// Memories are the per-unit retrieved memories (filtered) frozen into each
	// Todo's context_snapshot so M4/M5 replay the same background.
	Memories []map[string]any
}

type ResolvedCandidate struct {
	Candidate Candidate
	Semantic  SemanticResolution
}

type SemanticResolution struct {
	MatchedTodoID *uint64
	Vector        []float32
}

type PersistStats struct {
	Created int
	Updated int
	Todos   []TodoRef
	// Skipped counts candidates dropped because they are info-insufficient AND
	// their identity slot (dedup key) is empty, so no stable fingerprint exists.
	// Skipping one such candidate must not abort the whole batch (M3 是尽力抽取，
	// 单条线索缺关键身份就丢弃，不连累同批其它线索）。
	Skipped int
}

// TodoRef is the durable M3 handoff to M4. Status and version are captured after
// persistence so the downstream optimistic-lock claim targets the exact row M3
// committed rather than re-discovering work by timing.
type TodoRef struct {
	ID      uint64
	Version int32
	Status  string
}

type pipelineStore interface {
	LoadPendingChats(context.Context, LoadOptions) ([]ChatBatch, error)
	LoadPendingChat(context.Context, string, LoadOptions) (*ChatBatch, error)
	PersistChat(context.Context, ChatBatch, []UnitExtraction, string) (PersistStats, error)
}

type candidateDeduplicator interface {
	Resolve(context.Context, Candidate, *uint64) (SemanticResolution, error)
}
