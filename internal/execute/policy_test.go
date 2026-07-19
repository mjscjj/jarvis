package execute

import "testing"

func TestActionPolicies(t *testing.T) {
	cases := []struct {
		actionType   string
		wantSandbox  string
		wantExternal bool
	}{
		{"code_change", "workspace-write", false},
		{"investigate", "read-only", false},
		{"summary_post", "read-only", true},
		{"reply_message", "read-only", true},
		{"schedule_meeting", "read-only", true},
		{"doc_write", "read-only", true},
		{"manual_followup", "read-only", true},
	}
	for _, tc := range cases {
		t.Run(tc.actionType, func(t *testing.T) {
			p, ok := lookupPolicy(tc.actionType)
			if !ok {
				t.Fatalf("policy for %q not found", tc.actionType)
			}
			if p.sandbox != tc.wantSandbox {
				t.Fatalf("sandbox = %q, want %q", p.sandbox, tc.wantSandbox)
			}
			if p.external != tc.wantExternal {
				t.Fatalf("external = %v, want %v", p.external, tc.wantExternal)
			}
		})
	}
}

func TestUnknownActionPolicyFailsFast(t *testing.T) {
	if _, ok := lookupPolicy("no_such_action"); ok {
		t.Fatalf("unknown action_type must not resolve to a policy")
	}
}

func TestBuildExecutionPromptRequiresValidTask(t *testing.T) {
	if _, err := buildExecutionPrompt(nil, ""); err == nil {
		t.Fatalf("nil Task must fail")
	}
}
