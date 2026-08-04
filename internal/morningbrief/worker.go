// Package morningbrief implements Jarvis's daily morning planning brief.
//
// It is a Skill-driven one-shot agent, shaped like meetingsweep: Go only
// schedules, injects hard boundaries, runs the agent, and verifies that the
// canonical Markdown brief was refreshed. All judgment — evidence scope,
// capacity, ranking, delivery self-check — lives in the system prompt and
// summarize-morning-brief Skill. There is no run table: the artifact remains
// the source of truth and is exposed read-only to the frontend.
package morningbrief

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
)

// AgentStage is the provenance stage exported to jarvis-tools for this agent.
const AgentStage = "morning_brief"

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
}

type Worker struct {
	runner        Runner
	prompts       textstore.Reader
	sandbox       string
	workspaceRoot string
	location      *time.Location
	now           func() time.Time
}

func NewWorker(opts Options) (*Worker, error) {
	if opts.Runner == nil {
		return nil, fmt.Errorf("morning brief runner is nil")
	}
	if opts.Prompts == nil {
		return nil, fmt.Errorf("morning brief prompt reader is nil")
	}
	if strings.TrimSpace(opts.Sandbox) == "" {
		return nil, fmt.Errorf("morning brief sandbox is required")
	}
	workspaceRoot := strings.TrimSpace(opts.WorkspaceRoot)
	if workspaceRoot == "" {
		return nil, fmt.Errorf("morning brief workspace root is required")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("morning brief location is nil")
	}
	stat, err := os.Stat(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("stat morning brief workspace root %q: %w", workspaceRoot, err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("morning brief workspace root %q is not a directory", workspaceRoot)
	}
	return &Worker{
		runner: opts.Runner, prompts: opts.Prompts, sandbox: strings.TrimSpace(opts.Sandbox),
		workspaceRoot: workspaceRoot, location: opts.Location,
		now: time.Now,
	}, nil
}

func (w *Worker) RunOnce(ctx context.Context) (string, error) {
	return w.Run(ctx, TriggerManual)
}

func (w *Worker) Run(ctx context.Context, trigger string) (string, error) {
	if trigger != TriggerSchedule && trigger != TriggerManual {
		return "", fmt.Errorf("morning brief trigger must be schedule or manual")
	}
	// Wall clock anchors artifact freshness; w.now() only decides the business day.
	wallStarted := time.Now()
	now := w.now().In(w.location)
	systemPrompt, err := w.prompts.Content(ctx, textstore.SystemPromptMorningBriefKey)
	if err != nil {
		return "", fmt.Errorf("read morning brief system prompt: %w", err)
	}
	tools, err := toolcatalog.Block(toolcatalog.StageMorningBrief)
	if err != nil {
		return "", fmt.Errorf("build morning brief tool catalog: %w", err)
	}
	date := now.Format("2006-01-02")
	prompt := buildPrompt(systemPrompt, tools, now, date, trigger)
	result, err := w.runner.RunTextSandboxAtStage(ctx, prompt, w.sandbox, w.workspaceRoot, AgentStage)
	if err != nil {
		return "", fmt.Errorf("run morning brief agent: %w", err)
	}
	result = strings.TrimSpace(result)
	if result == "" {
		return "", fmt.Errorf("morning brief agent returned an empty final message")
	}
	if err := verifyBriefArtifact(w.briefPath(date), wallStarted); err != nil {
		return "", fmt.Errorf("%w; agent final message: %s", err, result)
	}
	return result, nil
}

// HasBriefFor reports whether the canonical brief for one local day is already on
// disk. The scheduler uses it so a restart-triggered catch-up does not spend a
// full strong-model run when today's brief was already written.
func (w *Worker) HasBriefFor(day time.Time) bool {
	stat, err := os.Stat(w.briefPath(day.In(w.location).Format("2006-01-02")))
	return err == nil && !stat.IsDir()
}

func (w *Worker) briefPath(date string) string {
	return filepath.Join(w.workspaceRoot, "data", "morning-brief", date, "99-brief.md")
}

func buildPrompt(systemPrompt, tools string, now time.Time, date, trigger string) string {
	parts := []string{strings.TrimSpace(systemPrompt)}
	if block := strings.TrimSpace(tools); block != "" {
		parts = append(parts, block)
	}
	deliverPolicy := "手动触发：只写本地 Markdown，一律不投递飞书。"
	if trigger == TriggerSchedule {
		deliverPolicy = "定时触发：生成后投递给 Principal 本人；若当天目录已有 delivered 记录，则只更新文件、不再重复投递。"
	}
	parts = append(parts,
		"BEGIN_MORNING_BRIEF\n"+
			"当前时间："+now.Format(time.RFC3339)+"\n"+
			"时区："+now.Location().String()+"\n"+
			"自然日："+date+"\n"+
			"trigger："+trigger+"\n"+
			"投递策略："+deliverPolicy+"\n"+
			"产物目录：data/morning-brief/"+date+"/\n"+
			"正式稿：data/morning-brief/"+date+"/99-brief.md\n"+
			"现在执行一轮晨间作战简报：先读取 summarize-morning-brief Skill，再严格按其步骤取证、写稿"+
			"（必要时投递）。不要创建 Task，不要给非 Principal 发消息，不要承诺不存在的交互入口。\n"+
			"END_MORNING_BRIEF",
	)
	return strings.Join(parts, "\n\n")
}

func verifyBriefArtifact(path string, startedAt time.Time) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("morning brief artifact %q: %w", path, err)
	}
	if stat.IsDir() {
		return fmt.Errorf("morning brief artifact %q is a directory", path)
	}
	// Allow equal second-resolution mtimes; reject only clearly stale files.
	if stat.ModTime().Before(startedAt) {
		return fmt.Errorf(
			"morning brief artifact %q was not refreshed by this run (mtime=%s started=%s)",
			path,
			stat.ModTime().Format(time.RFC3339Nano),
			startedAt.Format(time.RFC3339Nano),
		)
	}
	return nil
}
