package execute

import (
	"testing"
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
		{"notify_principal", "danger-full-access"},
		{"manual_followup", "danger-full-access"},
		{"some_novel_intent", "danger-full-access"},
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

func TestNovelActionTypeResolvesToDefaultPolicy(t *testing.T) {
	// action_type is an open set: any non-blank intent must remain executable,
	// running through the same execution/approval path (only code_change is special-cased).
	p, ok := lookupPolicy("no_such_action")
	if !ok {
		t.Fatalf("novel action_type must resolve to the default policy")
	}
	if p.sandbox != "danger-full-access" {
		t.Fatalf("sandbox = %q, want danger-full-access", p.sandbox)
	}
}

func TestBlankActionTypeFailsFast(t *testing.T) {
	if _, ok := lookupPolicy("  "); ok {
		t.Fatalf("blank action_type must not resolve to a policy")
	}
}

func TestBuildExecutionPromptRequiresValidTask(t *testing.T) {
	if _, err := buildExecutionPrompt("test M5 system prompt", "修改文件需要审批。", nil, "", testToolCatalog, "", "", "", nil); err == nil {
		t.Fatalf("nil Task must fail")
	}
}
