package execute

import (
	"context"
	"fmt"

	"jarvis/internal/domain"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
	"jarvis/internal/workrule"

	"gorm.io/gorm"
)

// Disposition is Codex's own verdict on an extracted Todo, returned verbatim in
// the decision schema (no longer re-inferred by us):
//   - ready: worth M5 investigating/executing → route auto.
//   - observe: worth keeping in view, but nobody has to act → route observing.
//   - drop: not worth doing → route dropped.
//
// A clue that needs the principal to choose or to supply a missing fact is still
// ready: the question travels with the Task in the decision payload, and M5
// raises it once it has done its own homework. observe is not a way to defer
// that question — it is for clues where acting is genuinely nobody's job.
const (
	DispositionReady   = "ready"
	DispositionObserve = "observe"
	DispositionDrop    = "drop"
)

// CodexEvaluator implements todoEvaluator by asking Codex (read-only) to judge
// each extracted Todo. It reuses the existing CodexDecider/BuildCodexPrompt/
// schema unchanged and maps Codex's disposition to the stored route. It also
// loads previous evaluation summaries from todo_event, so a re-run sees what the
// earlier pass concluded.
type CodexEvaluator struct {
	db        *gorm.DB // optional in unit tests; nil → empty previous_evaluations
	codex     codexDecisionRunner
	sharedMem sharedmem.SharedMemoryReader
	workRules workrule.Reader
	skills    skill.Reader
	prompts   textstore.Reader
}

func NewCodexEvaluator(db *gorm.DB, codex codexDecisionRunner, sharedMem sharedmem.SharedMemoryReader, workRules workrule.Reader, skills skill.Reader, prompts textstore.Reader) (*CodexEvaluator, error) {
	if codex == nil {
		return nil, fmt.Errorf("codex evaluator decider is nil")
	}
	if sharedMem == nil {
		return nil, fmt.Errorf("codex evaluator shared memory reader is nil")
	}
	if workRules == nil {
		return nil, fmt.Errorf("codex evaluator work rule reader is nil")
	}
	if skills == nil {
		return nil, fmt.Errorf("codex evaluator skill reader is nil")
	}
	if prompts == nil {
		return nil, fmt.Errorf("codex evaluator system prompt reader is nil")
	}
	return &CodexEvaluator{db: db, codex: codex, sharedMem: sharedMem, workRules: workRules, skills: skills, prompts: prompts}, nil
}

func (e *CodexEvaluator) Evaluate(ctx context.Context, todo *domain.Todo) (*EvaluationInput, error) {
	if todo == nil || todo.ID == 0 {
		return nil, fmt.Errorf("codex evaluator Todo is invalid")
	}
	if todo.Status != "extracted" {
		return nil, fmt.Errorf("codex evaluator Todo id=%d status=%s, want extracted", todo.ID, todo.Status)
	}

	// The decision step reuses the M3-frozen context_snapshot verbatim (no re-snapshot). It must
	// be present — fail-fast if empty (docs/design-context-pipeline.md §2.3).
	background, err := requireContextSnapshot(todo)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: %w", todo.ID, err)
	}
	prior, err := loadPriorEvaluations(ctx, e.db, todo.ID)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: %w", todo.ID, err)
	}
	sharedMemory, err := e.sharedMem.Text(ctx)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: read shared memory: %w", todo.ID, err)
	}
	workRules, err := e.workRules.Block(ctx, workrule.StageDecide)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: read decide work rules: %w", todo.ID, err)
	}
	skills, err := e.skills.Catalog(ctx, skill.StageDecide)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: read decide skills: %w", todo.ID, err)
	}
	systemPrompt, err := e.prompts.Content(ctx, textstore.SystemPromptDecisionKey)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: read decision system prompt: %w", todo.ID, err)
	}
	toolCatalog, err := toolcatalog.Block(toolcatalog.StageDecide)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: read decide tool catalog: %w", todo.ID, err)
	}
	prompt, err := BuildCodexPrompt(CodexPromptInput{
		Todo: todo, Background: background, PriorEvaluations: prior,
		SystemPrompt: systemPrompt, ToolCatalog: toolCatalog,
		SharedMemory: sharedMemory, WorkRules: workRules, Skills: skills,
	})
	if err != nil {
		return nil, fmt.Errorf("build codex decision prompt todo_id=%d: %w", todo.ID, err)
	}

	result, err := e.codex.Decide(ctx, CodexInput{Prompt: prompt.Text})
	if err != nil {
		return nil, fmt.Errorf("codex decision failed todo_id=%d: %w", todo.ID, err)
	}
	if result == nil {
		return nil, fmt.Errorf("codex decision returned nil result todo_id=%d", todo.ID)
	}

	disposition := result.Decision.Disposition
	route, err := routeForDisposition(disposition)
	if err != nil {
		return nil, fmt.Errorf("codex evaluation todo_id=%d: %w", todo.ID, err)
	}
	routeReason := "codex_" + disposition
	matchedRules := []string{"codex_disposition:" + disposition}

	sessionID := result.SessionID
	input := &EvaluationInput{
		TodoID:          todo.ID,
		ExpectedVersion: todo.Version,
		Route:           route,
		RouteReason:     routeReason,
		MatchedRules:    matchedRules,
		DecisionEngine:  DecisionEngineCodex,
		CodexSessionID:  &sessionID,
		PromptVersion:   prompt.Version,
		Plan:            result.Decision.Plan,
		DecisionPayload: result.Decision.Payload,
	}
	return input, nil
}

// routeForDisposition maps Codex's own disposition to the stored route:
//   - ready   → auto      (system creates the Task, M5 takes over)
//   - observe → observing (no Task, but the clue stays in view)
//   - drop    → dropped   (terminal, no Task)
func routeForDisposition(disposition string) (string, error) {
	switch disposition {
	case DispositionReady:
		return RouteAuto, nil
	case DispositionObserve:
		return RouteObserving, nil
	case DispositionDrop:
		return RouteDropped, nil
	default:
		return "", fmt.Errorf("unknown codex disposition %q", disposition)
	}
}
