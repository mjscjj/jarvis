package agentconfig

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type promptReader map[string]string

func (r promptReader) Content(_ context.Context, key string) (string, error) {
	value, ok := r[key]
	if !ok {
		return "", errors.New("missing prompt")
	}
	return value, nil
}

type ruleReader map[string]string

func (r ruleReader) Block(_ context.Context, stage string) (string, error) {
	return r[stage], nil
}

func TestPreviewUsesRuntimeTemplateRenderer(t *testing.T) {
	service, err := NewService(promptReader{
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
