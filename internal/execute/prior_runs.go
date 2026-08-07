package execute

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// loadPriorRunSummaries returns up to maxPriorRunsInPrompt earlier execution_run
// rows for a Task, oldest-first, for injection into the next M5 prompt. The run
// being started is already persisted as running by the time this is called, so
// currentRunID is excluded — the agent must not read its own empty row back as
// a prior attempt. Fail-fast on DB errors.
func (e *AgentExecutor) loadPriorRunSummaries(ctx context.Context, taskID, currentRunID uint64) ([]priorRunSummary, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("load prior runs: task_id must be positive")
	}
	list, err := e.store.ListRuns(ctx, taskID)
	if err != nil {
		return nil, err
	}
	items := make([]RunView, 0, len(list.Items))
	for _, item := range list.Items {
		if item.ID != currentRunID {
			items = append(items, item)
		}
	}
	return summarizePriorRuns(items, maxPriorRunsInPrompt), nil
}

// summarizePriorRuns converts RunView rows (newest-first from ListRuns) into
// prompt-facing summaries, keeping the newest `limit` and returning oldest-first.
func summarizePriorRuns(items []RunView, limit int) []priorRunSummary {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]priorRunSummary, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		out = append(out, runViewToPriorSummary(items[i]))
	}
	return out
}

func runViewToPriorSummary(run RunView) priorRunSummary {
	summary := priorRunSummary{
		RunID:     run.ID,
		Status:    run.Status,
		StartedAt: run.StartedAt.UTC().Format(time.RFC3339),
	}
	if run.Summary != nil {
		summary.Summary = strings.TrimSpace(*run.Summary)
	}
	if run.ErrorDetail != nil {
		summary.ErrorDetail = strings.TrimSpace(*run.ErrorDetail)
	}
	if len(run.Output) > 0 && string(run.Output) != "null" {
		summary.Output = json.RawMessage(run.Output)
	}
	if run.FinishedAt != nil {
		summary.FinishedAt = run.FinishedAt.UTC().Format(time.RFC3339)
	}
	return summary
}
