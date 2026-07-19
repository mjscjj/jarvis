package execute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var (
	// ErrExternalNeedsApproval is returned when an external-side-effect Task is
	// asked to run without explicit human approval. It is not a failure of the
	// Task; it is the safety gate. Callers decide whether to surface or skip.
	ErrExternalNeedsApproval = errors.New("external action requires human approval before execution")
	ErrUnknownActionType     = errors.New("unknown action_type has no execution policy")
)

// ExecuteInput drives one Task execution.
type ExecuteInput struct {
	TaskID uint64
	// ApproveExternal must be true to run an external-side-effect action. The
	// manual button sets it (the click is the approval); the cron auto-executor
	// leaves it false and skips external actions.
	ApproveExternal bool
}

// ExecuteResult summarizes what happened, for the API/log.
type ExecuteResult struct {
	TaskID     uint64 `json:"task_id"`
	RunID      uint64 `json:"run_id"`
	Status     string `json:"status"`
	Branch     string `json:"branch,omitempty"`
	Commit     string `json:"commit,omitempty"`
	DiffPath   string `json:"diff_path,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
}

// AgentExecutor is the M5 core. It does not hard-code a per-action workflow:
// it hands the Task's plan/background/slots and the repo path to codex and lets
// codex orchestrate, using action_type only to pick the sandbox and the
// external-approval gate.
type AgentExecutor struct {
	db       *gorm.DB
	store    *Store
	runner   *CodexRunner
	repoRoot string
	runsDir  string
	now      func() time.Time
}

func NewAgentExecutor(db *gorm.DB, store *Store, runner *CodexRunner, repoRoot, runsDir string) (*AgentExecutor, error) {
	if db == nil {
		return nil, fmt.Errorf("agent executor db is nil")
	}
	if store == nil {
		return nil, fmt.Errorf("agent executor store is nil")
	}
	if runner == nil {
		return nil, fmt.Errorf("agent executor codex runner is nil")
	}
	if strings.TrimSpace(repoRoot) == "" {
		return nil, fmt.Errorf("agent executor repo root is required")
	}
	if strings.TrimSpace(runsDir) == "" {
		return nil, fmt.Errorf("agent executor runs dir is required")
	}
	return &AgentExecutor{db: db, store: store, runner: runner, repoRoot: repoRoot, runsDir: runsDir, now: time.Now}, nil
}

// BatchStats summarizes one auto-execution sweep.
type BatchStats struct {
	Loaded   int
	Executed int
	Skipped  int
	Failed   int
}

// RunPendingBatch is the cron entry point: it executes pending local-action
// Tasks automatically and skips external-action Tasks (they need the manual
// approve). A single Task failure does not abort the sweep — it is counted and
// the loop continues, so one bad Task cannot block the rest.
func (e *AgentExecutor) RunPendingBatch(ctx context.Context, limit int) (BatchStats, error) {
	tasks, err := e.store.LoadPending(ctx, limit)
	if err != nil {
		return BatchStats{}, err
	}
	stats := BatchStats{Loaded: len(tasks)}
	for i := range tasks {
		task := &tasks[i]
		_, err := e.Execute(ctx, ExecuteInput{TaskID: task.ID, ApproveExternal: false})
		switch {
		case errors.Is(err, ErrExternalNeedsApproval):
			stats.Skipped++
		case err != nil:
			stats.Failed++
		default:
			stats.Executed++
		}
	}
	return stats, nil
}

func (e *AgentExecutor) Execute(ctx context.Context, input ExecuteInput) (*ExecuteResult, error) {
	if input.TaskID == 0 {
		return nil, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	var task domain.Task
	if err := e.db.WithContext(ctx).First(&task, input.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, input.TaskID)
		}
		return nil, fmt.Errorf("load Task id=%d: %w", input.TaskID, err)
	}

	policy, ok := lookupPolicy(task.ActionType)
	if !ok {
		return nil, fmt.Errorf("%w: task_id=%d action_type=%s", ErrUnknownActionType, task.ID, task.ActionType)
	}
	if policy.external && !input.ApproveExternal {
		return &ExecuteResult{
			TaskID: task.ID, Status: task.Status, Skipped: true,
			SkipReason: "external action requires approval",
		}, ErrExternalNeedsApproval
	}

	// Claim the Task (pending -> executing). This is the concurrency guard.
	execVersion, err := e.store.MarkExecuting(ctx, task.ID, task.Version)
	if err != nil {
		return nil, err
	}

	run, execErr := e.runOnce(ctx, &task, policy)

	// Persist the run record and finish the Task regardless of outcome.
	if writeErr := e.persistRun(ctx, run); writeErr != nil {
		return nil, fmt.Errorf("persist execution run task_id=%d: %w", task.ID, writeErr)
	}
	finishStatus := "done"
	if execErr != nil {
		finishStatus = "failed"
	}
	resultJSON, err := json.Marshal(runResultPayload(run, execErr))
	if err != nil {
		return nil, fmt.Errorf("encode execution result task_id=%d: %w", task.ID, err)
	}
	if _, err := e.store.Finish(ctx, FinishInput{
		TaskID: task.ID, ExpectedVersion: execVersion, Status: finishStatus, Result: resultJSON,
	}); err != nil {
		return nil, fmt.Errorf("finish Task id=%d after execution: %w", task.ID, err)
	}

	result := &ExecuteResult{
		TaskID: task.ID, RunID: run.ID, Status: finishStatus,
		Summary: derefString(run.Summary), Branch: derefString(run.Branch),
		Commit: derefString(run.Commit), DiffPath: derefString(run.DiffPath),
	}
	if execErr != nil {
		return result, fmt.Errorf("execute Task id=%d: %w", task.ID, execErr)
	}
	return result, nil
}

// runOnce builds the prompt, runs codex in the right sandbox, and (for code
// changes) creates a branch + commit + diff. It always returns a populated
// *domain.ExecutionRun; execErr is non-nil on any failure so the caller can
// mark the Task failed while still recording what was attempted.
func (e *AgentExecutor) runOnce(ctx context.Context, task *domain.Task, policy actionPolicy) (*domain.ExecutionRun, error) {
	startedAt := e.now().UTC()
	run := &domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Sandbox: policy.sandbox,
		Status: "running", StartedAt: startedAt,
	}

	var repoPath string
	var repo *gitRepo
	baseBranch := ""
	if task.ActionType == "code_change" {
		path, err := e.resolveRepo(task)
		if err != nil {
			return e.failRun(run, startedAt, err), err
		}
		repoPath = path
		run.RepoPath = &repoPath
		repo = newGitRepo(repoPath, 30*time.Second)
		if err := repo.ensureClean(ctx); err != nil {
			return e.failRun(run, startedAt, err), err
		}
		baseBranch, err = repo.currentBranch(ctx)
		if err != nil {
			return e.failRun(run, startedAt, err), err
		}
		branch := fmt.Sprintf("jarvis/task-%d", task.ID)
		if err := repo.createBranch(ctx, branch); err != nil {
			return e.failRun(run, startedAt, err), err
		}
		run.Branch = &branch
	}

	prompt, err := buildExecutionPrompt(task, repoPath)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	run.Prompt = prompt

	codexOut, err := e.runner.Run(ctx, prompt, policy.sandbox, repoPath)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	run.CodexSessionID = &codexOut.SessionID
	summary := codexOut.LastMessage
	run.Summary = &summary

	// For code changes, commit whatever codex did and capture the diff.
	if repo != nil {
		changed, err := repo.hasChanges(ctx)
		if err != nil {
			return e.failRun(run, startedAt, err), err
		}
		if changed {
			commit, err := repo.commitAll(ctx, fmt.Sprintf("jarvis: task %d — %s", task.ID, task.Title))
			if err != nil {
				return e.failRun(run, startedAt, err), err
			}
			run.Commit = &commit
			diff, err := repo.diffAgainst(ctx, baseBranch)
			if err != nil {
				return e.failRun(run, startedAt, err), err
			}
			diffPath, err := e.writeDiff(task.ID, diff)
			if err != nil {
				return e.failRun(run, startedAt, err), err
			}
			run.DiffPath = &diffPath
		}
	}

	finished := e.now().UTC()
	run.Status = "succeeded"
	run.FinishedAt = &finished
	ms := finished.Sub(startedAt).Milliseconds()
	run.DurationMs = &ms
	return run, nil
}

func (e *AgentExecutor) failRun(run *domain.ExecutionRun, startedAt time.Time, cause error) *domain.ExecutionRun {
	finished := e.now().UTC()
	detail := cause.Error()
	run.Status = "failed"
	run.ErrorDetail = &detail
	run.FinishedAt = &finished
	ms := finished.Sub(startedAt).Milliseconds()
	run.DurationMs = &ms
	if strings.TrimSpace(run.Prompt) == "" {
		run.Prompt = "(prompt not built)"
	}
	return run
}

func (e *AgentExecutor) persistRun(ctx context.Context, run *domain.ExecutionRun) error {
	return e.db.WithContext(ctx).Create(run).Error
}

// resolveRepo locates the local git repo for a code_change Task. It reads the
// repo_ref slot and joins it under the configured repo root. Fail-fast: a
// missing slot or a non-existent directory aborts rather than guessing.
func (e *AgentExecutor) resolveRepo(task *domain.Task) (string, error) {
	var slots map[string]any
	if err := json.Unmarshal(task.Slots, &slots); err != nil {
		return "", fmt.Errorf("decode Task slots task_id=%d: %w", task.ID, err)
	}
	ref, _ := slots["repo_ref"].(string)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("code_change Task id=%d is missing repo_ref slot", task.ID)
	}
	// Absolute repo_ref is honored as-is; otherwise join under repo root.
	path := ref
	if !filepath.IsAbs(ref) {
		path = filepath.Join(e.repoRoot, ref)
	}
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("resolved repo path %q is not a git repository", path)
	}
	return path, nil
}

func (e *AgentExecutor) writeDiff(taskID uint64, diff string) (string, error) {
	dir := filepath.Join(e.runsDir, fmt.Sprintf("task-%d", taskID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create runs dir %q: %w", dir, err)
	}
	path := filepath.Join(dir, fmt.Sprintf("change-%d.diff", e.now().UTC().UnixNano()))
	if err := os.WriteFile(path, []byte(diff), 0o644); err != nil {
		return "", fmt.Errorf("write diff %q: %w", path, err)
	}
	return path, nil
}

func runResultPayload(run *domain.ExecutionRun, execErr error) map[string]any {
	payload := map[string]any{
		"action_type": run.ActionType,
		"sandbox":     run.Sandbox,
		"run_status":  run.Status,
	}
	if run.Summary != nil {
		payload["summary"] = *run.Summary
	}
	if run.Branch != nil {
		payload["branch"] = *run.Branch
	}
	if run.Commit != nil {
		payload["commit"] = *run.Commit
	}
	if run.DiffPath != nil {
		payload["diff_path"] = *run.DiffPath
	}
	if run.CodexSessionID != nil {
		payload["codex_session_id"] = *run.CodexSessionID
	}
	if execErr != nil {
		payload["error"] = execErr.Error()
	}
	return payload
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
