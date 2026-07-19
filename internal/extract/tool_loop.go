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

// toolExtractor is the model transport that runs the function-calling loop. It
// is implemented by the provider client; kept as an interface so the worker is
// testable without a live endpoint.
type toolExtractor interface {
	ExtractWithTools(ctx context.Context, prompt Prompt, box ToolBox, maxRounds int) (*ExtractionResult, error)
}

// toolBoxBuilder builds the tool box for one conversation unit. It exists so the
// retrieval scope (memory filters, chat id) is bound per unit while the worker
// stays agnostic to concrete tool wiring.
type toolBoxBuilder interface {
	Build(batch ChatBatch, unit ConversationUnit) (ToolBox, error)
}
