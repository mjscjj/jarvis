package execute

import "context"

// Decision routes stored on a Todo (also used verbatim as the Todo.status).
//   - auto: the clue is worth pursuing, so the system creates the Task and M5
//     takes it from there.
//   - dropped: not worth doing; terminal, no Task.
//
// There is deliberately no "park the Todo until a human answers" route. When a
// clue needs a human — to pick between options or to supply a missing fact —
// the Task still gets created and carries that question; M5 raises it while
// executing (awaiting_approval / needs_human), so the only human gate lives on
// the Task.
const (
	RouteAuto    = "auto"
	RouteDropped = "dropped"
)

// DecisionEngineCodex is the sole engine recorded in the audit trail. The former
// "rule" engine scored confidence/risk against thresholds and the "manual" one
// parked every clue for human review; neither has an implementation left.
const DecisionEngineCodex = "codex"

// codexDecisionRunner is the read-only decision port implemented by CodexDecider
// and consumed by CodexEvaluator.
type codexDecisionRunner interface {
	Decide(context.Context, CodexInput) (*CodexResult, error)
}
