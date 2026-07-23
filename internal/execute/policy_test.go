package execute

import (
	"testing"

	"jarvis/internal/textstore"
)

func TestActionPolicies(t *testing.T) {
	cases := []struct {
		actionType  string
		wantSandbox string
	}{
		{"code_change", "danger-full-access"},
		{"investigate", "danger-full-access"},
		{"agent_task", "danger-full-access"},
		{"summary_post", "danger-full-access"},
		{"reply_message", "danger-full-access"},
		{"schedule_meeting", "danger-full-access"},
		{"doc_write", "danger-full-access"},
		{"manual_followup", "danger-full-access"},
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
		})
	}
}

func TestUnknownActionPolicyFailsFast(t *testing.T) {
	if _, ok := lookupPolicy("no_such_action"); ok {
		t.Fatalf("unknown action_type must not resolve to a policy")
	}
}

func TestBuildExecutionPromptRequiresValidTask(t *testing.T) {
	if _, err := buildExecutionPrompt(textstore.DefaultSystemPromptM5, nil, "", testToolCatalog, "", "", "", nil); err == nil {
		t.Fatalf("nil Task must fail")
	}
}
