package execute

import "context"

// Decision routes stored on a Todo (also used verbatim as the Todo.status).
//   - auto: the clue is worth pursuing, so the system creates the Task and M5
//     takes it from there.
//   - observing: worth keeping in view but asking nothing of anyone right now.
//     No Task. Not terminal: fresh evidence can put it back to extracted for a
//     real decision, and M5 can park a Task's clue here when it finds there is
//     nothing to do yet.
//   - dropped: not worth doing; terminal, no Task.
//
// There is deliberately no "park the Todo until a human answers" route. When a
// clue needs a human — to pick between options or to supply a missing fact —
// the Task still gets created and carries that question; M5 raises it while
// executing (awaiting_approval / needs_human), so the only human gate lives on
// the Task. observing is not that gate: it means nobody needs to act, not that
// we are waiting on the principal.
const (
	RouteAuto      = "auto"
	RouteObserving = "observing"
	RouteDropped   = "dropped"
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
