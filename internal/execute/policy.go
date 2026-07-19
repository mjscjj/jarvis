package execute

// actionPolicy describes how M5 should run a given action_type: which codex
// sandbox to use and whether the action touches the outside world.
//
// Boundary (decided with the user):
//   - Local actions (code_change, investigate) run automatically.
//   - External actions (anything that sends a message, books a meeting, writes a
//     doc, or otherwise reaches outside this machine) require a human to approve
//     execution first, even after M4 confirmation.
//   - Sandbox is danger-full-access for every action: this is a local trusted
//     host and external actions need lark-cli (Keychain + network). external
//     controls the human-approval gate, not the sandbox.
type actionPolicy struct {
	// sandbox is the codex sandbox level. On this trusted host every action runs
	// danger-full-access so external tools (lark-cli/bytedcli) can reach network
	// and macOS Keychain; the safety boundary is `external` (human approval).
	sandbox string
	// external is true when the action has outside-world side effects and must
	// be human-approved before it runs.
	external bool
}

// actionPolicies is the single source of truth for per-action execution rules.
// An unknown action_type is intentionally absent so lookups fail-fast.
var actionPolicies = map[string]actionPolicy{
	// Local, auto-executable.
	"code_change": {sandbox: "danger-full-access", external: false},
	"investigate": {sandbox: "danger-full-access", external: false},

	// External side effects — codex may draft, but execution needs approval.
	"summary_post":     {sandbox: "danger-full-access", external: true},
	"reply_message":    {sandbox: "danger-full-access", external: true},
	"schedule_meeting": {sandbox: "danger-full-access", external: true},
	"doc_write":        {sandbox: "danger-full-access", external: true},
	"manual_followup":  {sandbox: "danger-full-access", external: true},
}

func lookupPolicy(actionType string) (actionPolicy, bool) {
	p, ok := actionPolicies[actionType]
	return p, ok
}
