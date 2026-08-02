package execute

import (
	"encoding/json"
	"fmt"
	"strings"

	"jarvis/internal/domain"
	"jarvis/internal/sharedmem"
)

const CodexPromptVersion = "todo-decision-v6-value-gate"

type CodexPromptInput struct {
	Todo             *domain.Todo
	Background       json.RawMessage
	PriorEvaluations []PriorEvaluation
	SystemPrompt     string
	ToolCatalog      string
	SharedMemory     string
	WorkRules        string
	Skills           string
}

type CodexPrompt struct {
	Version string
	Text    string
}

func BuildCodexPrompt(input CodexPromptInput) (*CodexPrompt, error) {
	if input.Todo == nil || input.Todo.ID == 0 {
		return nil, fmt.Errorf("codex prompt Todo is invalid")
	}
	systemPrompt := strings.TrimSpace(input.SystemPrompt)

	// The decision step forwards M3's complete extraction and frozen context instead of
	// duplicating their semantic fields in a second rigid structure.
	extraction, err := canonicalNamedJSONObject(input.Todo.ExtractionResult, "codex extraction")
	if err != nil {
		return nil, fmt.Errorf("codex prompt todo id=%d: %w", input.Todo.ID, err)
	}
	background, err := canonicalNamedJSONObject(input.Background, "codex background")
	if err != nil {
		return nil, fmt.Errorf("codex prompt todo id=%d: %w", input.Todo.ID, err)
	}
	payload := codexPromptPayload{
		PromptVersion:       CodexPromptVersion,
		Extraction:          extraction,
		Background:          background,
		PreviousEvaluations: input.PriorEvaluations,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode codex decision prompt payload: %w", err)
	}

	var trusted []string
	for _, block := range []string{
		input.ToolCatalog,
		sharedmem.RenderBlock(input.SharedMemory),
		input.WorkRules,
		input.Skills,
	} {
		if block = strings.TrimSpace(block); block != "" {
			trusted = append(trusted, block)
		}
	}
	text := systemPrompt
	if len(trusted) > 0 {
		text += "\n\n" + strings.Join(trusted, "\n\n")
	}
	text += "\n\nDECISION_CONTEXT_LENGTH_BYTES=" + fmt.Sprintf("%d", len(encoded)) +
		"\nBEGIN_DECISION_CONTEXT\n" + string(encoded) + "\nEND_DECISION_CONTEXT"
	return &CodexPrompt{Version: CodexPromptVersion, Text: text}, nil
}

type codexPromptPayload struct {
	PromptVersion       string            `json:"prompt_version"`
	Extraction          json.RawMessage   `json:"extraction"`
	Background          json.RawMessage   `json:"background"`
	PreviousEvaluations []PriorEvaluation `json:"previous_evaluations,omitempty"`
}
