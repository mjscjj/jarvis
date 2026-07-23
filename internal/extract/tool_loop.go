package extract

import (
	"context"
	"encoding/json"

	"jarvis/internal/extract/tools"
)

// ToolBox is the per-unit tool surface driven by the extraction tool loop: the
// specs advertised to the model and the dispatch that runs a model tool call.
// *tools.Registry satisfies it.
type ToolBox interface {
	Specs() []tools.Spec
	Invoke(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error)
}

// ToolExtractor is the model transport that produces the extraction result. The
// kimi client implements it as a function-calling loop; the codex engine
// implements it as a single schema-constrained agent run. Kept as an interface
// so the worker is engine-agnostic and testable without a live endpoint.
type ToolExtractor interface {
	ExtractWithTools(ctx context.Context, prompt Prompt, box ToolBox) (*ExtractionResult, error)
}

// toolBoxBuilder builds the tool box for one conversation unit. It exists so the
// retrieval scope (memory filters, chat id) is bound per unit while the worker
// stays agnostic to concrete tool wiring.
type toolBoxBuilder interface {
	Build(batch ChatBatch, unit ConversationUnit) (ToolBox, error)
}
