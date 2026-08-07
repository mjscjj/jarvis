// Package agentconfig exposes stage-centric, read-only previews of the stable
// M3/M5 instructions. It composes the exact same file-backed templates and
// rules as runtime; dynamic task/session blocks remain explicitly listed rather
// than being fabricated in the admin UI.
package agentconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/prompttemplate"
	"jarvis/internal/textstore"
	"jarvis/internal/workrule"
)

var ErrStageNotFound = errors.New("agent config stage not found")

type Preview struct {
	Stage         string   `json:"stage"`
	Name          string   `json:"name"`
	Content       string   `json:"content"`
	DynamicBlocks []string `json:"dynamic_blocks"`
}

type Service struct {
	prompts textstore.Reader
	rules   workrule.Reader
}

func NewService(prompts textstore.Reader, rules workrule.Reader) (*Service, error) {
	if prompts == nil {
		return nil, fmt.Errorf("agent config prompt reader is nil")
	}
	if rules == nil {
		return nil, fmt.Errorf("agent config work rule reader is nil")
	}
	return &Service{prompts: prompts, rules: rules}, nil
}

func (s *Service) Preview(ctx context.Context, stage string) (*Preview, error) {
	stage = strings.TrimSpace(stage)
	var promptKey, ruleStage, name string
	var dynamicBlocks []string
	switch stage {
	case prompttemplate.StageM3:
		promptKey, ruleStage, name = textstore.SystemPromptM3Key, workrule.StageExtract, "线索发现"
		dynamicBlocks = []string{"principal_open_id", "tool_catalog", "shared_memory", "skills", "conversation_context", "output_contract"}
	case prompttemplate.StageM5:
		promptKey, ruleStage, name = textstore.SystemPromptM5Key, workrule.StageExecute, "任务执行"
		dynamicBlocks = []string{"phase_instructions", "shared_memory", "skills", "tool_catalog", "task_context", "output_schema"}
	default:
		return nil, fmt.Errorf("%w: %q", ErrStageNotFound, stage)
	}

	template, err := s.prompts.Content(ctx, promptKey)
	if err != nil {
		return nil, fmt.Errorf("read %s system prompt: %w", stage, err)
	}
	rules, err := s.rules.Block(ctx, ruleStage)
	if err != nil {
		return nil, fmt.Errorf("read %s work rules: %w", stage, err)
	}
	approvalPolicy := ""
	if stage == prompttemplate.StageM5 {
		approvalPolicy, err = s.prompts.Content(ctx, textstore.ApprovalPolicyKey)
		if err != nil {
			return nil, fmt.Errorf("read M5 approval policy: %w", err)
		}
	}
	content, err := prompttemplate.Render(stage, template, rules, approvalPolicy)
	if err != nil {
		return nil, fmt.Errorf("render %s preview: %w", stage, err)
	}
	return &Preview{Stage: stage, Name: name, Content: content, DynamicBlocks: dynamicBlocks}, nil
}
