package prompttemplate

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRequiresExactStagePlaceholders(t *testing.T) {
	for _, test := range []struct {
		name     string
		stage    string
		template string
		wantErr  bool
	}{
		{name: "m3", stage: StageM3, template: "role\n{{WORK_RULES}}"},
		{name: "m5", stage: StageM5, template: "role\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}"},
		{name: "missing", stage: StageM5, template: "role\n{{WORK_RULES}}", wantErr: true},
		{name: "duplicate", stage: StageM3, template: "{{WORK_RULES}}\n{{WORK_RULES}}", wantErr: true},
		{name: "unknown", stage: StageM3, template: "{{WORK_RULES}}\n{{TOOLS}}", wantErr: true},
		{name: "approval in m3", stage: StageM3, template: "{{WORK_RULES}}\n{{APPROVAL_POLICY}}", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(test.stage, test.template)
			if test.wantErr && !errors.Is(err, ErrInvalidTemplate) {
				t.Fatalf("Validate() error = %v, want ErrInvalidTemplate", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestRenderExpandsEachSourceExactlyOnce(t *testing.T) {
	rendered, err := Render(
		StageM5,
		"role\n{{WORK_RULES}}\npolicy follows\n{{APPROVAL_POLICY}}\nend",
		"BEGIN_WORK_RULES\nrule\nEND_WORK_RULES",
		"send needs approval",
	)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, want := range []string{"BEGIN_WORK_RULES", "rule", "BEGIN_APPROVAL_POLICY", "send needs approval"} {
		if strings.Count(rendered, want) != 1 {
			t.Fatalf("Render() count(%q) != 1:\n%s", want, rendered)
		}
	}
	if !strings.HasSuffix(rendered, "end") {
		t.Fatalf("Render() moved template suffix:\n%s", rendered)
	}
	if strings.Contains(rendered, "{{") {
		t.Fatalf("Render() left placeholder:\n%s", rendered)
	}
}

func TestRenderAllowsEmptyWorkRulesButRejectsEmptyM5Policy(t *testing.T) {
	if _, err := Render(StageM3, "role\n{{WORK_RULES}}", "", ""); err != nil {
		t.Fatalf("Render(M3 empty rules) error = %v", err)
	}
	if _, err := Render(StageM5, "role\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}", "", " "); !errors.Is(err, ErrInvalidTemplate) {
		t.Fatalf("Render(M5 empty policy) error = %v", err)
	}
}

func TestRenderDoesNotInterpretPlaceholdersInsideIncludedRules(t *testing.T) {
	rendered, err := Render(StageM3, "role\n{{WORK_RULES}}", "模板示例：{{VALUE}}", "")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(rendered, "{{VALUE}}") {
		t.Fatalf("Render() changed included rule content: %q", rendered)
	}
}
