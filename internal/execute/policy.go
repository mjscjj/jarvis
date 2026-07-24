package execute

import "strings"

// actionPolicy describes how M5 should run a given action_type.
//
// Boundary (decided with the user):
//   - code_change runs automatically and uses its MR as the review gate.
//   - Every other action first runs propose. The file-backed approval policy
//     decides whether its planned actions require human approval.
//   - Sandbox remains danger-full-access because read-side investigation may
//     need lark-cli/bytedcli network and Keychain access. The propose/apply state
//     machine is the approval boundary.
type actionPolicy struct {
	sandbox string
}

// defaultActionPolicy applies to every action_type. Because action_type is an
// OPEN set (M3 may emit any snake_case intent, e.g. notify_principal or a novel
// one), M5 does not gate execution on a closed allowlist. The sandbox stays
// danger-full-access for all types; the real safety boundary is the
// propose/approval state machine, which every non-code_change action passes
// through (code_change is special-cased elsewhere by its "code_change" label,
// using its MR as the review gate).
var defaultActionPolicy = actionPolicy{sandbox: "danger-full-access"}

// lookupPolicy returns the execution policy for an action_type. It accepts any
// non-blank type so novel intents remain executable; the second return value is
// kept for callers and is false only for a blank action_type (a real data bug).
func lookupPolicy(actionType string) (actionPolicy, bool) {
	if strings.TrimSpace(actionType) == "" {
		return actionPolicy{}, false
	}
	return defaultActionPolicy, true
}
