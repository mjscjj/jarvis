package agentconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/prompttemplate"
	"jarvis/internal/textstore"
	"jarvis/internal/workrule"
)

type promptReader map[string]string

func (r promptReader) Content(_ context.Context, key string) (string, error) {
	value, ok := r[key]
	if !ok {
		return "", errors.New("missing prompt")
	}
	return value, nil
}

func TestPreviewTracksSavedLevelsAndStageRules(t *testing.T) {
	repoPrompts, err := textstore.NewService("../../conf/prompts")
	if err != nil {
		t.Fatal(err)
	}
	files, err := repoPrompts.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(file.Path)), []byte(file.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prompts, err := textstore.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	rules := ruleReader{"extract": "M3_RULE_BEFORE", "execute": "M5_RULE_BEFORE"}
	service, err := NewService(prompts, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []string{"quiet", "normal", "active"} {
		if _, err := prompts.Update(t.Context(), textstore.InitiativeLevelKey, textstore.Input{Content: level}); err != nil {
			t.Fatal(err)
		}
		for _, stage := range []string{"m3", "m5", "proactive"} {
			preview, err := service.Preview(t.Context(), stage)
			if err != nil {
				t.Fatal(err)
			}
			key, rule, policy := textstore.SystemPromptProactiveKey, "", ""
			switch stage {
			case "m3":
				key, rule = textstore.SystemPromptM3Key, rules[workrule.StageExtract]
			case "m5":
				key, rule = textstore.SystemPromptM5Key, rules[workrule.StageExecute]
				policy, err = prompts.Content(t.Context(), textstore.ApprovalPolicyKey)
				if err != nil {
					t.Fatal(err)
				}
			}
			template, err := prompts.Content(t.Context(), key)
			if err != nil {
				t.Fatal(err)
			}
			want, err := prompttemplate.Render(stage, template, rule, policy, level)
			if err != nil {
				t.Fatal(err)
			}
			if preview.Content != want || preview.InitiativeLevel != level {
				t.Fatalf("%s preview differs from runtime at %s", stage, level)
			}
			if stage == "proactive" && (strings.Contains(preview.Content, "M3_RULE") || strings.Contains(preview.Content, "M5_RULE")) {
				t.Fatal("proactive inherited stage rules")
			}
		}
	}
	rules[workrule.StageExecute] = "M5_RULE_AFTER"
	preview, err := service.Preview(t.Context(), "m5")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Content, "M5_RULE_AFTER") || strings.Contains(preview.Content, "M5_RULE_BEFORE") {
		t.Fatal("preview kept stale rules")
	}
	if err := os.WriteFile(filepath.Join(dir, "initiative-level.md"), []byte("typo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Preview(t.Context(), "m5"); err == nil {
		t.Fatal("preview accepted invalid level")
	}
}

type ruleReader map[string]string

func (r ruleReader) Block(_ context.Context, stage string) (string, error) {
	return r[stage], nil
}

func TestPreviewUsesRuntimeTemplateRenderer(t *testing.T) {
	service, err := NewService(promptReader{
		"initiative_level":   "normal",
		"m3_system_prompt":   "M3\n{{WORK_RULES}}",
		"m5_system_prompt":   "M5\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}",
		"m5_approval_policy": "approve writes",
	}, ruleReader{"extract": "M3 rules", "execute": "M5 rules"})
	if err != nil {
		t.Fatal(err)
	}
	m5, err := service.Preview(t.Context(), "m5")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"M5 rules", "BEGIN_APPROVAL_POLICY", "approve writes"} {
		if !strings.Contains(m5.Content, want) {
			t.Fatalf("Preview(M5) missing %q:\n%s", want, m5.Content)
		}
	}
	if len(m5.DynamicBlocks) == 0 {
		t.Fatal("Preview(M5) must describe omitted runtime blocks")
	}
	if m5.Name != "任务执行" {
		t.Fatalf("Preview(M5) name = %q", m5.Name)
	}
	m3, err := service.Preview(t.Context(), "m3")
	if err != nil {
		t.Fatal(err)
	}
	if m3.Name != "线索发现" {
		t.Fatalf("Preview(M3) name = %q", m3.Name)
	}
	if _, err := service.Preview(t.Context(), "unknown"); !errors.Is(err, ErrStageNotFound) {
		t.Fatalf("Preview(unknown) error = %v", err)
	}
}
