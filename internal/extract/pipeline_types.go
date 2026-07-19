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
	ID         uint64
	ChatID     string
	Name       string
	IsKeyGroup bool
	ProjectID  *uint64
}

type ProjectContext struct {
	ID           uint64
	Code         string
	Name         string
	Role         string
	Description  string
	Repos        []byte
	KeyDecisions []byte
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
}

type pipelineStore interface {
	LoadPendingChats(context.Context, LoadOptions) ([]ChatBatch, error)
	PersistChat(context.Context, ChatBatch, []UnitExtraction, string) (PersistStats, error)
}

type modelExtractor interface {
	Extract(context.Context, Prompt) (*ExtractionResult, error)
}

type candidateDeduplicator interface {
	Resolve(context.Context, Candidate, *uint64) (SemanticResolution, error)
}
