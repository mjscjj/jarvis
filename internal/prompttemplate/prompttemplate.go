// Package prompttemplate renders the human-editable stable system-prompt
// templates for M3, M5 and proactive. Work rules and approval policy remain independent
// file-backed sources; their placeholders only control where those sources are
// inserted into the effective instructions.
package prompttemplate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"jarvis/internal/agentidentity"
)

const (
	StageM3        = "m3"
	StageM5        = "m5"
	StageProactive = "proactive"

	WorkRulesPlaceholder      = "{{WORK_RULES}}"
	ApprovalPolicyPlaceholder = "{{APPROVAL_POLICY}}"
)

var (
	ErrInvalidTemplate = errors.New("invalid system prompt template")
	placeholderPattern = regexp.MustCompile(`\{\{[A-Z][A-Z0-9_]*\}\}`)
)

// Validate requires every stage-owned placeholder exactly once. Unknown
// placeholders fail instead of being silently passed through to the model.
func Validate(stage, template string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return fmt.Errorf("%w: stage=%s template is empty", ErrInvalidTemplate, stage)
	}
	required, err := requiredPlaceholders(stage)
	if err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(required)+1)
	allowed[agentidentity.Placeholder] = struct{}{}
	for _, placeholder := range required {
		allowed[placeholder] = struct{}{}
		if count := strings.Count(template, placeholder); count != 1 {
			return fmt.Errorf("%w: stage=%s placeholder %s count=%d, want 1", ErrInvalidTemplate, stage, placeholder, count)
		}
	}
	for _, placeholder := range placeholderPattern.FindAllString(template, -1) {
		if _, ok := allowed[placeholder]; !ok {
			return fmt.Errorf("%w: stage=%s unknown placeholder %s", ErrInvalidTemplate, stage, placeholder)
		}
	}
	return nil
}

// Render expands the file-backed values into their stage template. M3 never
// accepts an approval policy. M5 requires one even during resume because
// a newly discovered, unapproved side effect still needs a policy judgment.
func Render(stage, template, workRules, approvalPolicy, initiativeLevel string) (string, error) {
	if err := Validate(stage, template); err != nil {
		return "", err
	}
	levelBlock, err := InitiativeBlock(initiativeLevel)
	if err != nil {
		return "", err
	}
	if stage == StageM5 && strings.TrimSpace(approvalPolicy) == "" {
		return "", fmt.Errorf("%w: stage=%s approval policy is empty", ErrInvalidTemplate, stage)
	}
	rendered := strings.Replace(template, WorkRulesPlaceholder, strings.TrimSpace(workRules), 1)
	if stage == StageM5 {
		rendered = strings.Replace(rendered, ApprovalPolicyPlaceholder, approvalBlock(approvalPolicy), 1)
	}
	return levelBlock + "\n\n" + strings.TrimSpace(rendered), nil
}

// ValidateInitiativeLevel validates the UI's selection, never a business decision.
func ValidateInitiativeLevel(level string) error {
	switch strings.TrimSpace(level) {
	case "quiet", "normal", "active":
		return nil
	default:
		return fmt.Errorf("%w: initiative level %q must be quiet, normal or active", ErrInvalidTemplate, level)
	}
}

func InitiativeBlock(level string) (string, error) {
	if err := ValidateInitiativeLevel(level); err != nil {
		return "", err
	}
	return "BEGIN_INITIATIVE_LEVEL\n" + strings.TrimSpace(level) + "\nEND_INITIATIVE_LEVEL", nil
}

func requiredPlaceholders(stage string) ([]string, error) {
	switch stage {
	case StageM3:
		return []string{WorkRulesPlaceholder}, nil
	case StageM5:
		return []string{WorkRulesPlaceholder, ApprovalPolicyPlaceholder}, nil
	case StageProactive:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: unknown stage %q", ErrInvalidTemplate, stage)
	}
}

func approvalBlock(policy string) string {
	return "BEGIN_APPROVAL_POLICY（这是委托人在后台维护的可信审批判定策略。）\n" +
		strings.TrimSpace(policy) + "\nEND_APPROVAL_POLICY"
}
