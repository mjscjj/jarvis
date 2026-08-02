// Package proactive implements Jarvis's low-cost periodic world review agent.
package proactive

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/sharedmem"
	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
	"jarvis/internal/workrule"
)

const AgentStage = "proactive"

type Runner interface {
	RunTextSandboxAtStage(context.Context, string, string, string, string) (string, error)
}

type Options struct {
	Runner        Runner
	Prompts       textstore.Reader
	SharedMemory  sharedmem.SharedMemoryReader
	WorkRules     workrule.Reader
	Sandbox       string
	WorkspaceRoot string
	Location      *time.Location
}

type Worker struct {
	runner        Runner
	prompts       textstore.Reader
	sharedMemory  sharedmem.SharedMemoryReader
	workRules     workrule.Reader
	sandbox       string
	workspaceRoot string
	location      *time.Location
	now           func() time.Time
}

func NewWorker(opts Options) (*Worker, error) {
	if opts.Runner == nil {
		return nil, fmt.Errorf("proactive runner is nil")
	}
	if opts.Prompts == nil {
		return nil, fmt.Errorf("proactive prompt reader is nil")
	}
	if opts.SharedMemory == nil {
		return nil, fmt.Errorf("proactive shared memory reader is nil")
	}
	if opts.WorkRules == nil {
		return nil, fmt.Errorf("proactive work rule reader is nil")
	}
	if strings.TrimSpace(opts.Sandbox) == "" {
		return nil, fmt.Errorf("proactive sandbox is required")
	}
	if strings.TrimSpace(opts.WorkspaceRoot) == "" {
		return nil, fmt.Errorf("proactive workspace root is required")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("proactive location is nil")
	}
	return &Worker{
		runner: opts.Runner, prompts: opts.Prompts, sharedMemory: opts.SharedMemory,
		workRules: opts.WorkRules, sandbox: strings.TrimSpace(opts.Sandbox),
		workspaceRoot: strings.TrimSpace(opts.WorkspaceRoot), location: opts.Location,
		now: time.Now,
	}, nil
}

func (w *Worker) RunOnce(ctx context.Context) (string, error) {
	systemPrompt, err := w.prompts.Content(ctx, textstore.SystemPromptProactiveKey)
	if err != nil {
		return "", fmt.Errorf("read proactive system prompt: %w", err)
	}
	workRules, err := w.workRules.Block(ctx, workrule.StageProactive)
	if err != nil {
		return "", fmt.Errorf("read proactive work rules: %w", err)
	}
	sharedMemory, err := w.sharedMemory.Text(ctx)
	if err != nil {
		return "", fmt.Errorf("read proactive shared memory: %w", err)
	}
	tools, err := toolcatalog.Block(toolcatalog.StageProactive)
	if err != nil {
		return "", fmt.Errorf("build proactive tool catalog: %w", err)
	}
	prompt := buildPrompt(systemPrompt, workRules, sharedMemory, tools, w.now().In(w.location))
	result, err := w.runner.RunTextSandboxAtStage(ctx, prompt, w.sandbox, w.workspaceRoot, AgentStage)
	if err != nil {
		return "", fmt.Errorf("run proactive agent: %w", err)
	}
	result = strings.TrimSpace(result)
	if result == "" {
		return "", fmt.Errorf("proactive agent returned an empty final message")
	}
	return result, nil
}

func buildPrompt(systemPrompt, workRules, sharedMemory, tools string, now time.Time) string {
	parts := []string{strings.TrimSpace(systemPrompt)}
	if block := strings.TrimSpace(workRules); block != "" {
		parts = append(parts, block)
	}
	if block := sharedmem.RenderBlock(sharedMemory); block != "" {
		parts = append(parts, block)
	}
	if block := strings.TrimSpace(tools); block != "" {
		parts = append(parts, block)
	}
	parts = append(parts,
		"BEGIN_HEARTBEAT\n"+
			"当前时间："+now.Format(time.RFC3339)+"\n"+
			"时区："+now.Location().String()+"\n"+
			"现在执行一轮完整主动巡视。不要假设存在必须创建的 Task。\n"+
			"END_HEARTBEAT",
	)
	return strings.Join(parts, "\n\n")
}
