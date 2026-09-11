// Package proactive implements Jarvis's low-cost periodic world review agent.
package proactive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/prompttemplate"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
)

const AgentStage = "proactive"

type Runner interface {
	RunTextSandboxAtStage(context.Context, string, string, string, string) (string, error)
}

type Options struct {
	Runner        Runner
	Recorder      Recorder
	Prompts       textstore.Reader
	SharedMemory  sharedmem.SharedMemoryReader
	Skills        skill.Reader
	Sandbox       string
	WorkspaceRoot string
	Location      *time.Location
	Engine        string
	Model         string
}

type Worker struct {
	runner        Runner
	recorder      Recorder
	prompts       textstore.Reader
	sharedMemory  sharedmem.SharedMemoryReader
	skills        skill.Reader
	sandbox       string
	workspaceRoot string
	location      *time.Location
	engine        string
	model         string
	now           func() time.Time
}

func NewWorker(opts Options) (*Worker, error) {
	if opts.Runner == nil {
		return nil, fmt.Errorf("proactive runner is nil")
	}
	if opts.Recorder == nil {
		return nil, fmt.Errorf("proactive run recorder is nil")
	}
	if opts.Prompts == nil {
		return nil, fmt.Errorf("proactive prompt reader is nil")
	}
	if opts.SharedMemory == nil {
		return nil, fmt.Errorf("proactive shared memory reader is nil")
	}
	if opts.Skills == nil {
		return nil, fmt.Errorf("proactive skill reader is nil")
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
	if strings.TrimSpace(opts.Engine) == "" || strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf("proactive engine and model are required")
	}
	return &Worker{
		runner: opts.Runner, recorder: opts.Recorder, prompts: opts.Prompts, sharedMemory: opts.SharedMemory, skills: opts.Skills,
		sandbox:       strings.TrimSpace(opts.Sandbox),
		workspaceRoot: strings.TrimSpace(opts.WorkspaceRoot), location: opts.Location,
		engine: strings.TrimSpace(opts.Engine), model: strings.TrimSpace(opts.Model),
		now: time.Now,
	}, nil
}

func (w *Worker) RunOnce(ctx context.Context) (string, error) {
	return w.Run(ctx, TriggerManual)
}

func (w *Worker) Run(ctx context.Context, trigger string) (string, error) {
	if trigger != TriggerSchedule && trigger != TriggerManual {
		return "", fmt.Errorf("proactive trigger must be schedule or manual")
	}
	systemPrompt, err := w.prompts.Content(ctx, textstore.SystemPromptProactiveKey)
	if err != nil {
		return "", fmt.Errorf("read proactive system prompt: %w", err)
	}
	initiativeLevel, err := w.prompts.Content(ctx, textstore.InitiativeLevelKey)
	if err != nil {
		return "", fmt.Errorf("read proactive initiative level: %w", err)
	}
	systemPrompt, err = prompttemplate.Render(prompttemplate.StageProactive, systemPrompt, "", "", initiativeLevel)
	if err != nil {
		return "", fmt.Errorf("render proactive system prompt: %w", err)
	}
	sharedMemory, err := w.sharedMemory.Text(ctx)
	if err != nil {
		return "", fmt.Errorf("read proactive shared memory: %w", err)
	}
	tools, err := toolcatalog.Block(toolcatalog.StageProactive)
	if err != nil {
		return "", fmt.Errorf("build proactive tool catalog: %w", err)
	}
	startedAt := w.now().UTC()
	skills, err := w.skills.Catalog(ctx, skill.StageProactive)
	if err != nil {
		return "", fmt.Errorf("read proactive skills: %w", err)
	}
	if strings.TrimSpace(skills) != "" {
		systemPrompt += "\n\n" + skills
	}
	prompt := buildPrompt(systemPrompt, sharedMemory, tools, startedAt.In(w.location))
	runID, err := w.recorder.Start(ctx, trigger, w.engine, w.model, prompt, startedAt)
	if err != nil {
		return "", fmt.Errorf("record proactive run start: %w", err)
	}
	result, err := w.runner.RunTextSandboxAtStage(ctx, prompt, w.sandbox, w.workspaceRoot, AgentStage)
	if err != nil {
		return "", w.recordFailure(ctx, runID, fmt.Errorf("run proactive agent: %w", err))
	}
	result = strings.TrimSpace(result)
	if result == "" {
		return "", w.recordFailure(ctx, runID, fmt.Errorf("proactive agent returned an empty final message"))
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := w.recorder.Succeed(finishCtx, runID, result, w.now().UTC()); err != nil {
		return "", fmt.Errorf("record proactive run success id=%d: %w", runID, err)
	}
	return result, nil
}

func (w *Worker) recordFailure(ctx context.Context, runID uint64, runErr error) error {
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := w.recorder.Fail(finishCtx, runID, runErr.Error(), w.now().UTC()); err != nil {
		return errors.Join(runErr, fmt.Errorf("record proactive run failure id=%d: %w", runID, err))
	}
	return runErr
}

func buildPrompt(systemPrompt, sharedMemory, tools string, localNow time.Time) string {
	parts := []string{strings.TrimSpace(systemPrompt)}
	if block := sharedmem.RenderBlock(sharedMemory); block != "" {
		parts = append(parts, block)
	}
	if block := strings.TrimSpace(tools); block != "" {
		parts = append(parts, block)
	}
	parts = append(parts,
		"BEGIN_HEARTBEAT\n"+
			"当前 UTC 时间（与 Task、ExecutionRun 等工具返回时间比较时只使用此值）："+localNow.UTC().Format(time.RFC3339)+"\n"+
			"当前业务本地时间（仅用于判断“今天”的自然日范围）："+localNow.Format(time.RFC3339)+"\n"+
			"业务时区："+localNow.Location().String()+"\n"+
			"现在执行一轮完整主动巡视。不要假设存在必须创建的 Task。\n"+
			"END_HEARTBEAT",
	)
	return strings.Join(parts, "\n\n")
}
