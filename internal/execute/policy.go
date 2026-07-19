package execute

// actionPolicy describes how M5 should run a given action_type: which codex
// sandbox to use and whether the action touches the outside world.
//
// Boundary (decided with the user):
//   - Local actions (code_change, investigate) run automatically.
//   - External actions (anything that sends a message, books a meeting, writes a
//     doc, or otherwise reaches outside this machine) require a human to approve
//     execution first, even after M4 confirmation.
type actionPolicy struct {
	// sandbox is the codex sandbox level: "workspace-write" for code changes,
	// "read-only" for everything else (investigate, drafting external content).
	sandbox string
	// external is true when the action has outside-world side effects and must
	// be human-approved before it runs.
	external bool
}

// actionPolicies is the single source of truth for per-action execution rules.
// An unknown action_type is intentionally absent so lookups fail-fast.
var actionPolicies = map[string]actionPolicy{
	// Local, auto-executable.
	"code_change": {sandbox: "workspace-write", external: false},
	"investigate": {sandbox: "read-only", external: false},

	// External side effects — codex may draft, but execution needs approval.
	"summary_post":     {sandbox: "read-only", external: true},
	"reply_message":    {sandbox: "read-only", external: true},
	"schedule_meeting": {sandbox: "read-only", external: true},
	"doc_write":        {sandbox: "read-only", external: true},
	"manual_followup":  {sandbox: "read-only", external: true},
}

func lookupPolicy(actionType string) (actionPolicy, bool) {
	p, ok := actionPolicies[actionType]
	return p, ok
}
