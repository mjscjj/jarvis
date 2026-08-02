package insight

import (
	"context"
	"fmt"
	"strings"
)

// TextRunner produces free-form text from a prompt. execute.CodexRunner.RunText
// satisfies it; kept as an interface so insight does not import execute.
type TextRunner interface {
	RunText(ctx context.Context, prompt string) (string, error)
}

// Summarizer turns an aggregated Digest into a short natural-language recap on
// demand. It has no cache and no cron: the UI calls it only when the user
// clicks "生成总结".
type Summarizer struct {
	runner TextRunner
}

func NewSummarizer(runner TextRunner) (*Summarizer, error) {
	if runner == nil {
		return nil, fmt.Errorf("summarizer runner is nil")
	}
	return &Summarizer{runner: runner}, nil
}

// Summarize returns a Chinese prose recap of the given digest window.
func (s *Summarizer) Summarize(ctx context.Context, digest *Digest) (string, error) {
	if digest == nil {
		return "", fmt.Errorf("digest is nil")
	}
	prompt := buildDigestPrompt(digest)
	text, err := s.runner.RunText(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("codex digest summarize: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("codex digest summarize returned empty text")
	}
	return text, nil
}

func buildDigestPrompt(digest *Digest) string {
	var b strings.Builder
	b.WriteString("你是我的工作助理。下面是我最近 ")
	fmt.Fprintf(&b, "%d 天的量化进展数据（按天）。请用中文写一段简洁的进展总结：先讲我个人的推进节奏和待处理压力，再讲重点核心群的活跃趋势。只根据数据说话，不要编造。控制在 200 字以内。\n\n", digest.Days)

	b.WriteString("# 我的进展（每天）\n")
	for _, day := range digest.Mine {
		fmt.Fprintf(&b, "- %s：新增交办Todo %d，生成任务 %d，完成任务 %d，失败 %d\n",
			day.Date, day.TodosCreated, day.TasksCreated, day.TasksDone, day.TasksFailed)
	}

	b.WriteString("\n# 重点核心群进展（每天）\n")
	if len(digest.KeyGroups) == 0 {
		b.WriteString("（暂无标记为核心群的会话）\n")
	}
	for _, group := range digest.KeyGroups {
		name := group.Name
		if name == "" {
			name = group.ChatID
		}
		fmt.Fprintf(&b, "## %s\n", name)
		for _, day := range group.Days {
			fmt.Fprintf(&b, "- %s：消息 %d 条，抽出Todo %d\n", day.Date, day.Messages, day.TodosExtracted)
		}
	}
	return b.String()
}
