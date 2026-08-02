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
	Location        *time.Location
}

// RegistryToolBoxBuilder builds a per-unit tools.Registry: query_chat_history and
// query_resources are both backed by the local tables. It holds the shared
// dependencies; Build binds the per-unit scope.
type RegistryToolBoxBuilder struct {
	db  *gorm.DB
	cfg ToolBoxConfig
}

// NewRegistryToolBoxBuilder validates dependencies and config fail-fast.
func NewRegistryToolBoxBuilder(db *gorm.DB, cfg ToolBoxConfig) (*RegistryToolBoxBuilder, error) {
	if db == nil {
		return nil, fmt.Errorf("tool box builder db is nil")
	}
	if cfg.ToolTimeout <= 0 {
		return nil, fmt.Errorf("tool box builder tool timeout must be positive")
	}
	if cfg.HistoryMaxLimit <= 0 {
		return nil, fmt.Errorf("tool box builder history max limit must be positive")
	}
	if cfg.Location == nil {
		return nil, fmt.Errorf("tool box builder location is nil")
	}
	return &RegistryToolBoxBuilder{db: db, cfg: cfg}, nil
}

// Build assembles the tool registry for one unit.
func (b *RegistryToolBoxBuilder) Build(_ ChatBatch, _ ConversationUnit) (ToolBox, error) {
	history, err := tools.NewQueryChatHistoryTool(b.db, b.cfg.ToolTimeout, b.cfg.HistoryMaxLimit, b.cfg.Location)
	if err != nil {
		return nil, err
	}
	resourceTool, err := tools.NewQueryResourcesTool(b.db, b.cfg.ToolTimeout, resourcesToolMaxLimit)
	if err != nil {
		return nil, err
	}
	registry, err := tools.NewRegistry(history, resourceTool)
	if err != nil {
		return nil, err
	}
	return registry, nil
}

// resourcesToolMaxLimit caps how many manually curated resources one
// query_resources call may return. It is a small fixed bound because the
// human-maintained resource set is tiny compared to chat history.
const resourcesToolMaxLimit = 20
