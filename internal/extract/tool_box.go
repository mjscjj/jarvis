package extract

import (
	"fmt"
	"time"

	"jarvis/internal/extract/tools"

	"gorm.io/gorm"
)

// ToolBoxConfig bounds the per-unit retrieval tools. Every field is validated
// fail-fast at construction so a misconfiguration is caught at startup.
type ToolBoxConfig struct {
	ToolTimeout     time.Duration
	HistoryMaxLimit int
	MemoryDefaultK  int
	MemoryMaxK      int
	MemoryThreshold float64
	Location        *time.Location
}

// RegistryToolBoxBuilder builds a per-unit tools.Registry: query_chat_history is
// backed by the local message table, search_memory is scoped to the unit's chat
// or project. It holds the shared dependencies; Build binds the per-unit scope.
type RegistryToolBoxBuilder struct {
	db     *gorm.DB
	memory memorySearcher
	cfg    ToolBoxConfig
}

// NewRegistryToolBoxBuilder validates dependencies and config fail-fast.
func NewRegistryToolBoxBuilder(db *gorm.DB, searcher memorySearcher, cfg ToolBoxConfig) (*RegistryToolBoxBuilder, error) {
	if db == nil {
		return nil, fmt.Errorf("tool box builder db is nil")
	}
	if searcher == nil {
		return nil, fmt.Errorf("tool box builder memory searcher is nil")
	}
	if cfg.ToolTimeout <= 0 {
		return nil, fmt.Errorf("tool box builder tool timeout must be positive")
	}
	if cfg.HistoryMaxLimit <= 0 {
		return nil, fmt.Errorf("tool box builder history max limit must be positive")
	}
	if cfg.MemoryDefaultK <= 0 {
		return nil, fmt.Errorf("tool box builder memory default top_k must be positive")
	}
	if cfg.MemoryMaxK < cfg.MemoryDefaultK {
		return nil, fmt.Errorf("tool box builder memory max top_k must be >= default top_k")
	}
	if cfg.MemoryThreshold < 0 || cfg.MemoryThreshold > 1 {
		return nil, fmt.Errorf("tool box builder memory threshold must be between 0 and 1")
	}
	if cfg.Location == nil {
		return nil, fmt.Errorf("tool box builder location is nil")
	}
	return &RegistryToolBoxBuilder{db: db, memory: searcher, cfg: cfg}, nil
}

// Build assembles the tool registry for one unit. The memory retrieval scope
// mirrors the worker's pre-retrieval: filter by project_id when the group is
// bound to a project, otherwise by chat_id.
func (b *RegistryToolBoxBuilder) Build(batch ChatBatch, _ ConversationUnit) (ToolBox, error) {
	history, err := tools.NewQueryChatHistoryTool(b.db, b.cfg.ToolTimeout, b.cfg.HistoryMaxLimit, b.cfg.Location)
	if err != nil {
		return nil, err
	}
	filters := map[string]any{"chat_id": batch.Group.ChatID}
	if batch.Group.ProjectID != nil {
		filters = map[string]any{"project_id": *batch.Group.ProjectID}
	}
	memoryTool, err := tools.NewSearchMemoryTool(
		b.memory, filters, b.cfg.MemoryDefaultK, b.cfg.MemoryMaxK, b.cfg.MemoryThreshold, b.cfg.ToolTimeout,
	)
	if err != nil {
		return nil, err
	}
	registry, err := tools.NewRegistry(history, memoryTool)
	if err != nil {
		return nil, err
	}
	return registry, nil
}
