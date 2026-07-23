package execute

// actionPolicy describes how M5 should run a given action_type.
//
// Boundary (decided with the user):
//   - code_change runs automatically and uses its MR as the review gate.
//   - Every other action first runs propose. Pure reads may finish; any local or
//     external mutation requires human approval.
//   - Sandbox remains danger-full-access because read-side investigation may
//     need lark-cli/bytedcli network and Keychain access. The propose/apply state
//     machine is the approval boundary.
type actionPolicy struct {
	sandbox string
}

// actionPolicies is the allowlist of executable action types and their sandbox.
// Mutation approval is intent-based in propose, not a static action label.
var actionPolicies = map[string]actionPolicy{
	"code_change":      {sandbox: "danger-full-access"},
	"investigate":      {sandbox: "danger-full-access"},
	"agent_task":       {sandbox: "danger-full-access"},
	"summary_post":     {sandbox: "danger-full-access"},
	"reply_message":    {sandbox: "danger-full-access"},
	"schedule_meeting": {sandbox: "danger-full-access"},
	"doc_write":        {sandbox: "danger-full-access"},
	"manual_followup":  {sandbox: "danger-full-access"},
}

func lookupPolicy(actionType string) (actionPolicy, bool) {
	p, ok := actionPolicies[actionType]
	return p, ok
}
