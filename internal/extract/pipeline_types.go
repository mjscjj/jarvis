package extract

import (
	"context"
	"time"

	"jarvis/internal/agentusage"
	"jarvis/internal/contextsnap"
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
	RecentTaskLimit int
}

type GroupContext struct {
	ID             uint64
	ChatID         string
	Name           string
	Description    string // group announcement; a strong signal for project attribution
	BackgroundNote string // human-curated context that complements the group announcement
	IsKeyGroup     bool
	ProjectID      *uint64
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

// PrincipalContext is the principal ("me") background fed to the model so
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
	// PersonID is the person-table id when this open_id is enrolled; nil means
	// the speaker is unknown to the roster and cannot contribute person facts.
	PersonID *uint64
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
	// AssignerOpenID / AssignerPersonID are not rendered in the prompt; they
	// feed key-person fact loading (交办人 ∪ leaders ∪ speakers).
	AssignerOpenID   *string
	AssignerPersonID *uint64
}

type ConversationUnit struct {
	Key          string
	Messages     []MessageContext
	Participants []ParticipantContext
	Resources    []ResourceContext
}

// RecentTaskContext is the thin progress projection pushed into the M3 prompt.
// Detail lives behind get-task; here we only show what moved recently.
type RecentTaskContext struct {
	ID             uint64
	Title          string
	Status         string
	Summary        string
	LastProgressAt string // RFC3339; empty when unknown
}

type ChatBatch struct {
	Group         GroupContext
	Project       *ProjectContext
	OtherProjects []OtherProjectContext
	Principal     *PrincipalContext
	OpenTodos     []OpenTodoContext
	RecentTasks   []RecentTaskContext
	Units         []ConversationUnit
	LastNew       MessageContext
	// NewMessageCount includes every message advanced by this batch, including
	// non-extractable message types that still move the M3 watermark.
	NewMessageCount int
}

type UnitExtraction struct {
	UnitKey    string
	Candidates []ResolvedCandidate
	// Facts are the already-distilled facts about this chat's group and project,
	// frozen into each Todo's context_snapshot for audit and M5 on-demand lookup.
	Facts []contextsnap.Fact
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

// TodoRef is the durable M3 handoff to Task materialization. Status and version
// are captured after persistence so the downstream optimistic-lock claim targets the exact row M3
// committed rather than re-discovering work by timing.
type TodoRef struct {
	ID      uint64
	Version int32
	Status  string
}

type pipelineStore interface {
	PendingChatIDs(context.Context) ([]string, error)
	LoadPendingChat(context.Context, string, LoadOptions) (*ChatBatch, error)
	LoadChatMessages(context.Context, string, []string) ([]MessageContext, error)
	StartExtractionRun(context.Context, string, time.Time) (uint64, error)
	FinishExtractionRun(context.Context, uint64, ExtractionRunFinish) error
	PersistChat(context.Context, ChatBatch, []UnitExtraction, string) (PersistStats, error)
}

type ExtractionRunFinish struct {
	Status       string
	MessageCount int64
	TodoCount    int64
	Usage        agentusage.Usage
	ErrorDetail  *string
	FinishedAt   time.Time
}

type candidateDeduplicator interface {
	Resolve(context.Context, Candidate, *uint64) (SemanticResolution, error)
}
