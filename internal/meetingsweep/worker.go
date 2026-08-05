// Package meetingsweep implements Jarvis's low-cost periodic meeting collector.
//
// Meetings and future calendar events produce no chat message, so nothing wakes
// M3 for them. This agent is the missing first breath: it periodically searches
// recently ended Feishu meetings and upcoming meeting events, then delivers
// each one as a clue via jarvis-tools append-clue. The clue lands in the shared
// evidence stream and wakes the existing M2 -> M3 -> M5 pipeline.
//
// It is a pure collector, not an analyser: it does not fetch minutes, judge
// recordings, or write summaries — those are M3/M5's job. Its whole behaviour
// lives in the system prompt plus the two meeting clue Skills, so unlike the
// proactive agent it needs neither shared memory nor work rules, and it records
// no run table because its output is observable as clues and Todos.
package meetingsweep

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
)

// AgentStage is the provenance stage exported to jarvis-tools for this agent.
const AgentStage = "meeting_sweep"

const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// Runner runs one text prompt in a sandbox rooted at a workspace and returns the
// agent's final message. execute.CodexRunner satisfies it.
type Runner interface {
	RunTextSandboxAtStage(context.Context, string, string, string, string) (string, error)
}

type Options struct {
	Runner        Runner
	Prompts       textstore.Reader
	Sandbox       string
	WorkspaceRoot string
	Location      *time.Location
	Engine        string
	Model         string
}

type Worker struct {
	runner        Runner
	prompts       textstore.Reader
	sandbox       string
	workspaceRoot string
	location      *time.Location
	engine        string
	model         string
	now           func() time.Time
}

func NewWorker(opts Options) (*Worker, error) {
	if opts.Runner == nil {
		return nil, fmt.Errorf("meeting sweep runner is nil")
	}
	if opts.Prompts == nil {
		return nil, fmt.Errorf("meeting sweep prompt reader is nil")
	}
	if strings.TrimSpace(opts.Sandbox) == "" {
		return nil, fmt.Errorf("meeting sweep sandbox is required")
	}
	if strings.TrimSpace(opts.WorkspaceRoot) == "" {
		return nil, fmt.Errorf("meeting sweep workspace root is required")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("meeting sweep location is nil")
	}
	if strings.TrimSpace(opts.Engine) == "" || strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf("meeting sweep engine and model are required")
	}
	return &Worker{
		runner: opts.Runner, prompts: opts.Prompts, sandbox: strings.TrimSpace(opts.Sandbox),
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
		return "", fmt.Errorf("meeting sweep trigger must be schedule or manual")
	}
	systemPrompt, err := w.prompts.Content(ctx, textstore.SystemPromptMeetingSweepKey)
	if err != nil {
		return "", fmt.Errorf("read meeting sweep system prompt: %w", err)
	}
	tools, err := toolcatalog.Block(toolcatalog.StageMeetingSweep)
	if err != nil {
		return "", fmt.Errorf("build meeting sweep tool catalog: %w", err)
	}
	now := w.now().In(w.location)
	prompt := buildPrompt(systemPrompt, tools, now)
	result, err := w.runner.RunTextSandboxAtStage(ctx, prompt, w.sandbox, w.workspaceRoot, AgentStage)
	if err != nil {
		return "", fmt.Errorf("run meeting sweep agent: %w", err)
	}
	result = strings.TrimSpace(result)
	if result == "" {
		return "", fmt.Errorf("meeting sweep agent returned an empty final message")
	}
	return result, nil
}

func buildPrompt(systemPrompt, tools string, now time.Time) string {
	parts := []string{strings.TrimSpace(systemPrompt)}
	if block := strings.TrimSpace(tools); block != "" {
		parts = append(parts, block)
	}
	parts = append(parts,
		"BEGIN_HEARTBEAT\n"+
			"当前时间："+now.Format(time.RFC3339)+"\n"+
			"时区："+now.Location().String()+"\n"+
			"现在执行一轮会议巡扫：回看最近结束的会议，并向前扫描未来 24 小时的待参加会议，逐场投递客观线索。两个时间窗都没有会议就直接说明，不要硬造。\n"+
			"END_HEARTBEAT",
	)
	return strings.Join(parts, "\n\n")
}
