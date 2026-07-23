package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/observability"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/taskcreate"
	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
	"jarvis/internal/workrule"

	"code.byted.org/middleware/hertz/pkg/common/hlog"
	"gorm.io/gorm"
)

var (
	ErrUnknownActionType    = errors.New("unknown action_type has no execution policy")
	ErrExecutionInterrupted = errors.New("execution interrupted by user")
)

// ExecuteInput drives one Task execution (the propose stage).
type ExecuteInput struct {
	TaskID uint64
}

// ExecuteResult summarizes what happened, for the API/log. When Status is
// awaiting_approval, codex identified a required mutation and produced a
// proposal without performing it; the Task is parked for human approval.
type ExecuteResult struct {
	TaskID          uint64 `json:"task_id"`
	RunID           uint64 `json:"run_id"`
	Status          string `json:"status"`
	Branch          string `json:"branch,omitempty"`
	Commit          string `json:"commit,omitempty"`
	DiffPath        string `json:"diff_path,omitempty"`
	MergeRequestURL string `json:"merge_request_url,omitempty"`
	Summary         string `json:"summary,omitempty"`
}

// AgentExecutor is the execution core. It does not hard-code a per-action
// workflow: it hands the Task's plan/background/slots and the repo path to codex
// and lets codex orchestrate. action_type only picks the sandbox and whether the
// run skips the propose/approval gate (code_change) or must propose first.
type AgentExecutor struct {
	db        *gorm.DB
	store     *Store
	runner    *CodexRunner
	sharedMem sharedmem.SharedMemoryReader
	workRules workrule.Reader
	textStore textstore.Reader
	skills    skill.Reader
	repoRoot  string
	runsDir   string
	now       func() time.Time
	activeMu  sync.Mutex
	active    map[uint64]*activeExecution
}

type activeExecution struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
}

func NewAgentExecutor(db *gorm.DB, store *Store, runner *CodexRunner, sharedMem sharedmem.SharedMemoryReader, workRules workrule.Reader, textStore textstore.Reader, skills skill.Reader, repoRoot, runsDir string) (*AgentExecutor, error) {
	if db == nil {
		return nil, fmt.Errorf("agent executor db is nil")
	}
	if store == nil {
		return nil, fmt.Errorf("agent executor store is nil")
	}
	if runner == nil {
		return nil, fmt.Errorf("agent executor codex runner is nil")
	}
	if sharedMem == nil {
		return nil, fmt.Errorf("agent executor shared memory reader is nil")
	}
	if workRules == nil {
		return nil, fmt.Errorf("agent executor work rule reader is nil")
	}
	if textStore == nil {
		return nil, fmt.Errorf("agent executor text storage reader is nil")
	}
	if skills == nil {
		return nil, fmt.Errorf("agent executor skill reader is nil")
	}
	if strings.TrimSpace(repoRoot) == "" {
		return nil, fmt.Errorf("agent executor repo root is required")
	}
	if strings.TrimSpace(runsDir) == "" {
		return nil, fmt.Errorf("agent executor runs dir is required")
	}
	return &AgentExecutor{
		db: db, store: store, runner: runner, sharedMem: sharedMem, workRules: workRules, textStore: textStore, skills: skills,
		repoRoot: repoRoot, runsDir: runsDir,
		now: time.Now, active: make(map[uint64]*activeExecution),
	}, nil
}

func (e *AgentExecutor) beginExecution(parent context.Context, taskID uint64) (context.Context, *activeExecution, error) {
	if taskID == 0 {
		return nil, nil, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	runCtx, cancel := context.WithCancelCause(parent)
	active := &activeExecution{cancel: cancel, done: make(chan struct{})}
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	if e.active == nil {
		e.active = make(map[uint64]*activeExecution)
	}
	if _, exists := e.active[taskID]; exists {
		cancel(nil)
		return nil, nil, fmt.Errorf("%w: task_id=%d already has an active process", ErrInvalidTransition, taskID)
	}
	e.active[taskID] = active
	return runCtx, active, nil
}

func (e *AgentExecutor) endExecution(taskID uint64, active *activeExecution) {
	e.activeMu.Lock()
	if e.active[taskID] == active {
		delete(e.active, taskID)
		close(active.done)
	}
	e.activeMu.Unlock()
}

func (e *AgentExecutor) abandonExecution(taskID uint64, active *activeExecution) {
	active.cancel(nil)
	e.endExecution(taskID, active)
}

func (e *AgentExecutor) runInBackground(taskID uint64, runCtx context.Context, active *activeExecution, run func(context.Context) error) {
	go func() {
		defer e.endExecution(taskID, active)
		if err := run(runCtx); err != nil {
			hlog.CtxErrorf(runCtx, "background execution failed task_id=%d error=%+v", taskID, err)
		}
	}()
}

// Interrupt stops the live Codex process for one executing Task and waits until
// its normal audit/finalization path records a terminal interrupted result.
// External effects completed before the interrupt are not rolled back.
func (e *AgentExecutor) Interrupt(ctx context.Context, taskID uint64, expectedVersion int32) (*ExecuteResult, error) {
	if taskID == 0 || expectedVersion < 0 {
		return nil, fmt.Errorf("%w: task_id/version is invalid", ErrInvalidInput)
	}
	var task domain.Task
	if err := e.db.WithContext(ctx).First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return nil, fmt.Errorf("load Task id=%d before interrupt: %w", taskID, err)
	}
	if task.Version != expectedVersion {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, expectedVersion, task.Version)
	}
	if task.Status != "executing" {
		return nil, fmt.Errorf("%w: task_id=%d status=%s cannot be interrupted", ErrInvalidTransition, task.ID, task.Status)
	}

	e.activeMu.Lock()
	active := e.active[taskID]
	e.activeMu.Unlock()
	if active == nil {
		result, err := json.Marshal(map[string]any{
			"stage": "interrupted",
			"error": "未找到仍在运行的 Codex 进程，任务已标记为打断（服务可能在执行期间重启）",
		})
		if err != nil {
			return nil, fmt.Errorf("encode inactive interrupt result task_id=%d: %w", taskID, err)
		}
		if _, err := e.store.Finish(ctx, FinishInput{
			TaskID: taskID, ExpectedVersion: expectedVersion, Status: "failed", Result: result,
			ActorType: "user", EventType: "execution_interrupted",
		}); err != nil {
			return nil, fmt.Errorf("finish inactive interrupted Task id=%d: %w", taskID, err)
		}
		return &ExecuteResult{TaskID: taskID, Status: "failed", Summary: "执行已由用户打断（未发现活跃进程）"}, nil
	}
	active.cancel(ErrExecutionInterrupted)
	select {
	case <-active.done:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for task_id=%d interrupt finalization: %w", taskID, ctx.Err())
	}

	var finished domain.Task
	if err := e.db.WithContext(ctx).First(&finished, taskID).Error; err != nil {
		return nil, fmt.Errorf("reload Task id=%d after interrupt: %w", taskID, err)
	}
	if finished.Status != "failed" || !resultHasStage(finished.ExecutionResult, "interrupted") {
		return nil, fmt.Errorf("%w: task_id=%d completed as status=%s before interrupt took effect", ErrInvalidTransition, taskID, finished.Status)
	}
	return &ExecuteResult{TaskID: taskID, Status: "failed", Summary: "执行已由用户打断"}, nil
}

// KickExecute starts Task execution in the background. It claims the Task
// synchronously (pending -> executing) so the API/UI immediately see executing,
// then runs codex in a goroutine. Poll Task status for completion.
func (e *AgentExecutor) KickExecute(ctx context.Context, input ExecuteInput) (*ExecuteResult, error) {
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
	if task.Status != "pending" {
		return nil, fmt.Errorf("%w: task_id=%d from=%s to=executing", ErrInvalidTransition, task.ID, task.Status)
	}
	if err := validateTaskIntegrity(&task); err != nil {
		return nil, err
	}
	if _, ok := lookupPolicy(task.ActionType); !ok {
		return nil, fmt.Errorf("%w: task_id=%d action_type=%s", ErrUnknownActionType, task.ID, task.ActionType)
	}
	runCtx, active, err := e.beginExecution(observability.Detached(ctx), task.ID)
	if err != nil {
		return nil, err
	}
	execVersion, err := e.store.MarkExecuting(ctx, task.ID, task.Version)
	if err != nil {
		e.abandonExecution(task.ID, active)
		return nil, err
	}
	e.executeClaimedInBackground(runCtx, active, task.ID, execVersion)
	return &ExecuteResult{TaskID: task.ID, Status: "executing"}, nil
}

// KickRerun re-executes an already-finished Task (done or failed): it resets the
// Task to pending (clearing the old result), claims it as executing, and runs in
// the background. Returns status=executing so the UI can refresh immediately.
func (e *AgentExecutor) KickRerun(ctx context.Context, taskID uint64) (*ExecuteResult, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	var current domain.Task
	if err := e.db.WithContext(ctx).First(&current, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return nil, fmt.Errorf("load Task id=%d before rerun: %w", taskID, err)
	}
	if err := validateTaskIntegrity(&current); err != nil {
		return nil, err
	}
	if _, ok := lookupPolicy(current.ActionType); !ok {
		return nil, fmt.Errorf("%w: task_id=%d action_type=%s", ErrUnknownActionType, current.ID, current.ActionType)
	}
	task, err := e.store.ResetForRerun(ctx, taskID)
	if err != nil {
		return nil, err
	}
	runCtx, active, err := e.beginExecution(observability.Detached(ctx), task.ID)
	if err != nil {
		return nil, err
	}
	execVersion, err := e.store.MarkExecuting(ctx, task.ID, task.Version)
	if err != nil {
		e.abandonExecution(task.ID, active)
		return nil, err
	}
	e.executeClaimedInBackground(runCtx, active, task.ID, execVersion)
	return &ExecuteResult{TaskID: taskID, Status: "executing"}, nil
}

// KickReapply re-lands the SAME human-approved proposal for a Task whose apply
// stage previously failed (failed -> executing), WITHOUT going back through
// propose/approval. It recovers the last approved proposal from the run history,
// claims the Task, and runs the apply stage in the background. It fails-fast if
// no approved proposal is recoverable (the Task never went through approval — the
// caller should use rerun instead). Poll Task status for completion.
func (e *AgentExecutor) KickReapply(ctx context.Context, taskID uint64) (*ExecuteResult, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	var task domain.Task
	if err := e.db.WithContext(ctx).First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return nil, fmt.Errorf("load Task id=%d: %w", taskID, err)
	}
	if task.Status != "failed" {
		return nil, fmt.Errorf("%w: task_id=%d status=%s cannot re-apply (only failed apply attempts)", ErrInvalidTransition, task.ID, task.Status)
	}
	policy, ok := lookupPolicy(task.ActionType)
	if !ok {
		return nil, fmt.Errorf("%w: task_id=%d action_type=%s", ErrUnknownActionType, task.ID, task.ActionType)
	}
	proposal, err := e.store.LastApprovedProposal(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if proposal == nil {
		return nil, fmt.Errorf("%w: task_id=%d has no approved proposal to re-apply (use rerun)", ErrInvalidTransition, task.ID)
	}
	runCtx, active, err := e.beginExecution(observability.Detached(ctx), task.ID)
	if err != nil {
		return nil, err
	}
	execVersion, err := e.store.ClaimForReapply(ctx, task.ID, task.Version)
	if err != nil {
		e.abandonExecution(task.ID, active)
		return nil, err
	}
	claimed := task
	e.runInBackground(task.ID, runCtx, active, func(runCtx context.Context) error {
		_, err := e.applyApproved(runCtx, &claimed, policy, proposal, execVersion)
		return err
	})
	return &ExecuteResult{TaskID: task.ID, Status: "executing"}, nil
}

func (e *AgentExecutor) executeClaimedInBackground(runCtx context.Context, active *activeExecution, taskID uint64, execVersion int32) {
	e.runInBackground(taskID, runCtx, active, func(runCtx context.Context) error {
		_, err := e.executeClaimed(runCtx, taskID, execVersion)
		return err
	})
}

// ResumeTask is called by a one-time resume_task schedule. It claims the parked
// Task synchronously, then continues the exact persisted Codex session in the
// background so the scheduler itself never owns a long model invocation.
func (e *AgentExecutor) ResumeTask(ctx context.Context, taskID, sourceRunID uint64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: resume reason is required", ErrInvalidInput)
	}
	systemPrompt, err := e.textStore.Content(ctx, textstore.SystemPromptM5Key)
	if err != nil {
		return fmt.Errorf("load M5 waiting resume system prompt: %w", err)
	}
	workRules, err := e.workRules.Block(ctx, workrule.StageExecute)
	if err != nil {
		return fmt.Errorf("load M5 work rules for waiting resume: %w", err)
	}
	toolCatalog, err := toolcatalog.Block(toolcatalog.StageExecute)
	if err != nil {
		return fmt.Errorf("load M5 waiting resume tool catalog: %w", err)
	}
	prompt, err := buildScheduledResumePrompt(systemPrompt, reason, workRules, toolCatalog)
	if err != nil {
		return err
	}
	runCtx, active, err := e.beginExecution(observability.Detached(ctx), taskID)
	if err != nil {
		return err
	}
	execVersion, err := e.store.ClaimWaiting(ctx, taskID, sourceRunID)
	if err != nil {
		e.abandonExecution(taskID, active)
		return err
	}
	e.runInBackground(taskID, runCtx, active, func(runCtx context.Context) error {
		_, err := e.resumeClaimed(runCtx, taskID, sourceRunID, prompt, execVersion)
		return err
	})
	return nil
}

// KickResumeAfterHuman continues the exact Codex session that requested human
// input. It does not rerun the Task or rebuild the approved proposal.
func (e *AgentExecutor) KickResumeAfterHuman(ctx context.Context, taskID uint64, expectedVersion int32, response string) (*ExecuteResult, error) {
	systemPrompt, err := e.textStore.Content(ctx, textstore.SystemPromptM5Key)
	if err != nil {
		return nil, fmt.Errorf("load M5 human resume system prompt: %w", err)
	}
	workRules, err := e.workRules.Block(ctx, workrule.StageExecute)
	if err != nil {
		return nil, fmt.Errorf("load M5 work rules for human resume: %w", err)
	}
	toolCatalog, err := toolcatalog.Block(toolcatalog.StageExecute)
	if err != nil {
		return nil, fmt.Errorf("load M5 human resume tool catalog: %w", err)
	}
	prompt, err := buildHumanResumePrompt(systemPrompt, response, workRules, toolCatalog)
	if err != nil {
		return nil, err
	}
	runCtx, active, err := e.beginExecution(observability.Detached(ctx), taskID)
	if err != nil {
		return nil, err
	}
	claim, err := e.store.ClaimNeedsHuman(ctx, taskID, expectedVersion, response, "backend")
	if err != nil {
		e.abandonExecution(taskID, active)
		return nil, err
	}
	e.runInBackground(taskID, runCtx, active, func(runCtx context.Context) error {
		_, err := e.resumeClaimed(runCtx, claim.TaskID, claim.SourceRunID, prompt, claim.Version)
		return err
	})
	return &ExecuteResult{TaskID: claim.TaskID, Status: "executing"}, nil
}

func (e *AgentExecutor) resumeClaimed(ctx context.Context, taskID, sourceRunID uint64, prompt string, execVersion int32) (*ExecuteResult, error) {
	var task domain.Task
	if err := e.db.WithContext(context.WithoutCancel(ctx)).First(&task, taskID).Error; err != nil {
		return nil, fmt.Errorf("load resumed Task id=%d: %w", taskID, err)
	}
	var source domain.ExecutionRun
	if err := e.db.WithContext(context.WithoutCancel(ctx)).First(&source, sourceRunID).Error; err != nil {
		return nil, fmt.Errorf("load resumed source run id=%d: %w", sourceRunID, err)
	}
	if source.TaskID != task.ID || source.CodexSessionID == nil || strings.TrimSpace(*source.CodexSessionID) == "" {
		return nil, fmt.Errorf("%w: source_run_id=%d has no persisted Codex session for task_id=%d", ErrInvalidInput, sourceRunID, taskID)
	}
	policy, ok := lookupPolicy(task.ActionType)
	if !ok {
		return nil, fmt.Errorf("%w: task_id=%d action_type=%s", ErrUnknownActionType, task.ID, task.ActionType)
	}
	stage := source.Stage
	if stage == "" {
		stage = "execute"
	}
	sch := schemaExecution
	if stage == "propose" {
		sch = schemaPropose
	}
	startedAt := e.now().UTC()
	run := &domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Stage: stage, Sandbox: policy.sandbox,
		Status: "running", Prompt: prompt, StartedAt: startedAt,
	}
	if errors.Is(context.Cause(ctx), ErrExecutionInterrupted) {
		e.failRun(run, startedAt, ErrExecutionInterrupted)
		if writeErr := e.persistRun(ctx, run); writeErr != nil {
			return nil, fmt.Errorf("persist interrupted resumed run task_id=%d: %w", task.ID, writeErr)
		}
		return e.finishRun(ctx, &task, execVersion, run, ErrExecutionInterrupted)
	}
	repoPath := ""
	if source.RepoPath != nil {
		repoPath = strings.TrimSpace(*source.RepoPath)
		run.RepoPath = &repoPath
	}
	run.BaseBranch = source.BaseBranch
	run.Branch = source.Branch
	var repo *gitRepo
	baseBranch := ""
	if task.ActionType == "code_change" && repoPath != "" {
		if source.BaseBranch == nil || strings.TrimSpace(*source.BaseBranch) == "" ||
			source.Branch == nil || strings.TrimSpace(*source.Branch) == "" {
			execErr := fmt.Errorf("%w: waiting code_change run id=%d is missing persisted Git branches", ErrInvalidInput, source.ID)
			e.failRun(run, startedAt, execErr)
			if writeErr := e.persistRun(ctx, run); writeErr != nil {
				return nil, fmt.Errorf("persist failed resumed run task_id=%d: %w", task.ID, writeErr)
			}
			return e.finishRun(ctx, &task, execVersion, run, execErr)
		}
		baseBranch = strings.TrimSpace(*source.BaseBranch)
		repo = newGitRepo(repoPath, 30*time.Second)
		currentBranch, err := repo.currentBranch(ctx)
		if err != nil {
			e.failRun(run, startedAt, err)
			if writeErr := e.persistRun(ctx, run); writeErr != nil {
				return nil, fmt.Errorf("persist failed resumed run task_id=%d: %w", task.ID, writeErr)
			}
			return e.finishRun(ctx, &task, execVersion, run, err)
		}
		if currentBranch != strings.TrimSpace(*source.Branch) {
			execErr := fmt.Errorf("resume code_change task_id=%d: repo branch changed while waiting: expected=%s actual=%s", task.ID, *source.Branch, currentBranch)
			e.failRun(run, startedAt, execErr)
			if writeErr := e.persistRun(ctx, run); writeErr != nil {
				return nil, fmt.Errorf("persist failed resumed run task_id=%d: %w", task.ID, writeErr)
			}
			return e.finishRun(ctx, &task, execVersion, run, execErr)
		}
	}
	outputCapture, err := e.prepareTaskRunOutput(task.ID, stage, startedAt, prompt)
	if err != nil {
		e.failRun(run, startedAt, err)
		if writeErr := e.persistRun(ctx, run); writeErr != nil {
			return nil, fmt.Errorf("persist failed resumed run task_id=%d: %w", task.ID, writeErr)
		}
		return e.finishRun(ctx, &task, execVersion, run, err)
	}
	codexOut, execErr := e.runner.ResumeTaskWithOutput(ctx, *source.CodexSessionID, prompt, policy.sandbox, repoPath, sch, task.ID, outputCapture)
	if execErr != nil {
		e.failRun(run, startedAt, execErr)
		if writeErr := e.persistRun(ctx, run); writeErr != nil {
			return nil, fmt.Errorf("persist failed resumed run task_id=%d: %w", task.ID, writeErr)
		}
		return e.finishRun(ctx, &task, execVersion, run, execErr)
	}
	run.CodexSessionID = &codexOut.SessionID
	if stage == "propose" {
		if codexOut.Propose == nil {
			execErr = fmt.Errorf("resumed codex propose returned no structured result")
			e.failRun(run, startedAt, execErr)
		} else {
			result := codexOut.Propose
			run.Summary = &result.Summary
			run.Output, err = json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("encode resumed propose result task_id=%d: %w", task.ID, err)
			}
			run.Status = "succeeded"
			if result.Outcome == "waiting" {
				e.finishWaitingRun(run, startedAt)
			} else if result.Outcome == "needs_human" && !result.NeedsApproval {
				e.finishNeedsHumanRun(run, startedAt)
			} else if result.Outcome == "failed" {
				execErr = fmt.Errorf("task not completed: %s", result.FailureReason)
				e.failRun(run, startedAt, execErr)
			} else {
				e.finishSuccessfulRun(run, startedAt)
			}
			if writeErr := e.persistRun(ctx, run); writeErr != nil {
				return nil, fmt.Errorf("persist resumed propose run task_id=%d: %w", task.ID, writeErr)
			}
			if execErr != nil || result.Outcome == "waiting" {
				return e.finishRun(ctx, &task, execVersion, run, execErr)
			}
			if result.NeedsApproval {
				proposalJSON, err := json.Marshal(proposalPayload(run, result))
				if err != nil {
					return nil, fmt.Errorf("encode resumed proposal task_id=%d: %w", task.ID, err)
				}
				if _, err := e.store.MarkAwaitingApproval(ctx, task.ID, execVersion, run.ID, proposalJSON); err != nil {
					return nil, err
				}
				return &ExecuteResult{TaskID: task.ID, RunID: run.ID, Status: "awaiting_approval", Summary: result.Summary}, nil
			}
			return e.finishRun(ctx, &task, execVersion, run, nil)
		}
	} else {
		if codexOut.Result == nil {
			execErr = fmt.Errorf("resumed codex exec returned no structured result")
			e.failRun(run, startedAt, execErr)
		} else {
			result := codexOut.Result
			run.Summary = &result.Summary
			run.Output, err = json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("encode resumed execution result task_id=%d: %w", task.ID, err)
			}
			if result.Outcome == "waiting" {
				e.finishWaitingRun(run, startedAt)
			} else if result.Outcome == "needs_human" {
				e.finishNeedsHumanRun(run, startedAt)
			} else if result.Outcome == "completed" {
				if repo != nil {
					execErr = e.finalizeCodeChange(ctx, &task, run, repo, baseBranch)
				}
				if execErr != nil {
					e.failRun(run, startedAt, execErr)
				} else {
					e.finishSuccessfulRun(run, startedAt)
				}
			} else {
				execErr = fmt.Errorf("task not completed: %s", result.FailureReason)
				e.failRun(run, startedAt, execErr)
			}
		}
	}
	if writeErr := e.persistRun(ctx, run); writeErr != nil {
		return nil, fmt.Errorf("persist resumed run task_id=%d: %w", task.ID, writeErr)
	}
	return e.finishRun(ctx, &task, execVersion, run, execErr)
}

func buildScheduledResumePrompt(systemPrompt, reason, workRules, toolCatalog string) (string, error) {
	systemPrompt = strings.TrimSpace(systemPrompt)
	reason = strings.TrimSpace(reason)
	if systemPrompt == "" || reason == "" {
		return "", fmt.Errorf("waiting resume system prompt and reason are required")
	}
	prompt := systemPrompt + "\n\n" + m5PhaseResumeWaiting
	if block := strings.TrimSpace(workRules); block != "" {
		prompt += "\n\n" + block
	}
	if block := strings.TrimSpace(toolCatalog); block != "" {
		prompt += "\n\n" + block
	}
	return fmt.Sprintf("%s\n\n等待原因：%s\n当前时间：%s", prompt, reason, time.Now().UTC().Format(time.RFC3339)), nil
}

func buildHumanResumePrompt(systemPrompt, response, workRules, toolCatalog string) (string, error) {
	systemPrompt = strings.TrimSpace(systemPrompt)
	response = strings.TrimSpace(response)
	if systemPrompt == "" || response == "" {
		return "", fmt.Errorf("human resume system prompt and response are required")
	}
	prompt := systemPrompt + "\n\n" + m5PhaseResumeHuman
	if block := strings.TrimSpace(workRules); block != "" {
		prompt += "\n\n" + block
	}
	if block := strings.TrimSpace(toolCatalog); block != "" {
		prompt += "\n\n" + block
	}
	return fmt.Sprintf("%s\n\n委托人回应：%s\n当前时间：%s", prompt, response, time.Now().UTC().Format(time.RFC3339)), nil
}

func (e *AgentExecutor) finishSuccessfulRun(run *domain.ExecutionRun, startedAt time.Time) {
	finished := e.now().UTC()
	run.Status = "succeeded"
	run.FinishedAt = &finished
	ms := finished.Sub(startedAt).Milliseconds()
	run.DurationMs = &ms
}

func (e *AgentExecutor) finishWaitingRun(run *domain.ExecutionRun, startedAt time.Time) {
	finished := e.now().UTC()
	run.Status = "waiting"
	run.FinishedAt = &finished
	ms := finished.Sub(startedAt).Milliseconds()
	run.DurationMs = &ms
}

func (e *AgentExecutor) finishNeedsHumanRun(run *domain.ExecutionRun, startedAt time.Time) {
	finished := e.now().UTC()
	run.Status = "needs_human"
	run.FinishedAt = &finished
	ms := finished.Sub(startedAt).Milliseconds()
	run.DurationMs = &ms
}

// KickApprove lands an accepted proposal in the background. It synchronously
// validates and claims the awaiting_approval Task (-> executing, under optimistic
// lock) so version/state conflicts surface immediately to the caller, then runs
// the apply stage (a fresh codex invocation) in a goroutine and returns at once.
// The apply codex call can be slow; the HTTP handler must not block on it. Poll
// Task status or refresh the list for completion.
func (e *AgentExecutor) KickApprove(ctx context.Context, taskID uint64, expectedVersion int32) (*ExecuteResult, error) {
	runCtx, active, err := e.beginExecution(observability.Detached(ctx), taskID)
	if err != nil {
		return nil, err
	}
	task, policy, proposal, execVersion, err := e.claimForApproval(ctx, taskID, expectedVersion)
	if err != nil {
		e.abandonExecution(taskID, active)
		return nil, err
	}
	e.runInBackground(taskID, runCtx, active, func(runCtx context.Context) error {
		_, err := e.applyApproved(runCtx, task, policy, proposal, execVersion)
		return err
	})
	return &ExecuteResult{TaskID: task.ID, Status: "executing"}, nil
}

// Approve lands a proposal that a human accepted, synchronously. It claims the
// awaiting_approval Task (-> executing), rebuilds a fresh codex invocation with
// the approved proposal embedded in the prompt (the apply stage — codex exec
// --ephemeral cannot resume the propose session, so this is a new run that
// faithfully lands the already-decided artifact), and finishes the Task
// done/failed on the real external write's verdict. Prefer KickApprove from HTTP
// handlers; this stays for callers that need to block on the outcome (tests).
func (e *AgentExecutor) Approve(ctx context.Context, taskID uint64, expectedVersion int32) (*ExecuteResult, error) {
	task, policy, proposal, execVersion, err := e.claimForApproval(ctx, taskID, expectedVersion)
	if err != nil {
		return nil, err
	}
	return e.applyApproved(ctx, task, policy, proposal, execVersion)
}

// claimForApproval validates an awaiting_approval Task, decodes its stored
// proposal, and atomically claims it (awaiting_approval -> executing) under
// optimistic lock. It is the synchronous prefix shared by Approve and
// KickApprove so version/state conflicts fail fast before any codex work starts.
func (e *AgentExecutor) claimForApproval(ctx context.Context, taskID uint64, expectedVersion int32) (*domain.Task, actionPolicy, *codexProposal, int32, error) {
	if taskID == 0 {
		return nil, actionPolicy{}, nil, 0, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	var task domain.Task
	if err := e.db.WithContext(ctx).First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, actionPolicy{}, nil, 0, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return nil, actionPolicy{}, nil, 0, fmt.Errorf("load Task id=%d: %w", taskID, err)
	}
	if task.Version != expectedVersion {
		return nil, actionPolicy{}, nil, 0, fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, expectedVersion, task.Version)
	}
	if task.Status != "awaiting_approval" {
		return nil, actionPolicy{}, nil, 0, fmt.Errorf("%w: task_id=%d status=%s cannot be approved", ErrInvalidTransition, task.ID, task.Status)
	}
	policy, ok := lookupPolicy(task.ActionType)
	if !ok {
		return nil, actionPolicy{}, nil, 0, fmt.Errorf("%w: task_id=%d action_type=%s", ErrUnknownActionType, task.ID, task.ActionType)
	}
	proposal, err := decodeStoredProposal(task.ExecutionResult)
	if err != nil {
		return nil, actionPolicy{}, nil, 0, fmt.Errorf("read stored proposal task_id=%d: %w", task.ID, err)
	}
	execVersion, err := e.store.MarkExecutingFromApproval(ctx, task.ID, task.Version)
	if err != nil {
		return nil, actionPolicy{}, nil, 0, err
	}
	return &task, policy, proposal, execVersion, nil
}

// applyApproved runs the apply stage for an already-claimed Task and finishes it
// done/failed on the real external write's verdict. It is the shared tail of
// Approve (synchronous) and KickApprove (background goroutine).
func (e *AgentExecutor) applyApproved(ctx context.Context, task *domain.Task, policy actionPolicy, proposal *codexProposal, execVersion int32) (*ExecuteResult, error) {
	run, execErr := e.runApply(ctx, task, policy, proposal)
	execErr = e.normalizeInterrupted(ctx, run, execErr)
	if writeErr := e.persistRun(ctx, run); writeErr != nil {
		return nil, fmt.Errorf("persist execution run task_id=%d: %w", task.ID, writeErr)
	}
	return e.finishRun(ctx, task, execVersion, run, execErr)
}

// Reject declines a proposed external write. It moves the awaiting_approval Task
// to failed and records the rejection (optionally with a reason) in
// execution_result so the UI shows why; the Task can later be rerun to re-propose.
func (e *AgentExecutor) Reject(ctx context.Context, taskID uint64, expectedVersion int32, reason string) (*ExecuteResult, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	var task domain.Task
	if err := e.db.WithContext(ctx).First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return nil, fmt.Errorf("load Task id=%d: %w", taskID, err)
	}
	if task.Version != expectedVersion {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, expectedVersion, task.Version)
	}
	if task.Status != "awaiting_approval" {
		return nil, fmt.Errorf("%w: task_id=%d status=%s cannot be rejected", ErrInvalidTransition, task.ID, task.Status)
	}
	resultJSON, err := json.Marshal(rejectionPayload(strings.TrimSpace(reason)))
	if err != nil {
		return nil, fmt.Errorf("encode rejection task_id=%d: %w", task.ID, err)
	}
	if _, err := e.store.RejectAwaitingApproval(ctx, task.ID, task.Version, resultJSON); err != nil {
		return nil, err
	}
	return &ExecuteResult{TaskID: task.ID, Status: "failed", Summary: "已驳回外部写入方案"}, nil
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

	if _, ok := lookupPolicy(task.ActionType); !ok {
		return nil, fmt.Errorf("%w: task_id=%d action_type=%s", ErrUnknownActionType, task.ID, task.ActionType)
	}
	if err := validateTaskIntegrity(&task); err != nil {
		return nil, err
	}
	runCtx, active, err := e.beginExecution(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	defer e.endExecution(task.ID, active)

	// Claim the Task (pending -> executing). This is the concurrency guard.
	execVersion, err := e.store.MarkExecuting(ctx, task.ID, task.Version)
	if err != nil {
		return nil, err
	}
	return e.executeClaimed(runCtx, task.ID, execVersion)
}

// executeClaimed runs an already-claimed (executing) Task. Used by KickExecute /
// KickRerun after a synchronous claim, and by Execute after MarkExecuting.
func (e *AgentExecutor) executeClaimed(ctx context.Context, taskID uint64, execVersion int32) (*ExecuteResult, error) {
	var task domain.Task
	if err := e.db.WithContext(context.WithoutCancel(ctx)).First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return nil, fmt.Errorf("load Task id=%d: %w", taskID, err)
	}
	policy, ok := lookupPolicy(task.ActionType)
	if !ok {
		return nil, fmt.Errorf("%w: task_id=%d action_type=%s", ErrUnknownActionType, task.ID, task.ActionType)
	}

	// Only code_change runs straight through to completion (edit + commit + diff +
	// push + open MR): the MR is a natural second review gate — nothing merges
	// without a human. Every OTHER action_type runs the propose stage first,
	// regardless of execution_mode. The file-backed approval policy decides
	// whether the proposed plan may execute or must stop for human approval.
	if runsToCompletion(&task) {
		run, execErr := e.runOnce(ctx, &task, policy)
		execErr = e.normalizeInterrupted(ctx, run, execErr)
		if writeErr := e.persistRun(ctx, run); writeErr != nil {
			return nil, fmt.Errorf("persist execution run task_id=%d: %w", task.ID, writeErr)
		}
		return e.finishRun(ctx, &task, execVersion, run, execErr)
	}
	return e.executePropose(ctx, &task, policy, execVersion)
}

// runsToCompletion reports whether a Task skips the propose/approval gate. Only
// code_change qualifies because its pushed branch + MR is the review gate.
// execution_mode=direct is retained as persisted metadata but grants no approval
// bypass: all non-code tasks must pass through propose and its approval policy.
func runsToCompletion(task *domain.Task) bool {
	return task != nil && task.ActionType == "code_change"
}

func validateTaskIntegrity(task *domain.Task) error {
	if task == nil || task.ID == 0 {
		return fmt.Errorf("%w: Task is invalid", ErrInvalidInput)
	}
	switch task.ExecutionMode {
	case taskcreate.ExecutionModeStandard, taskcreate.ExecutionModeDirect:
	default:
		return fmt.Errorf("%w: task_id=%d unknown execution_mode=%q", ErrInvalidInput, task.ID, task.ExecutionMode)
	}
	hash, err := taskcreate.ActionHash(task.ActionType, task.Target, json.RawMessage(task.Plan))
	if err != nil {
		return fmt.Errorf("%w: task_id=%d action integrity: %v", ErrInvalidInput, task.ID, err)
	}
	if hash != task.ActionHash {
		return fmt.Errorf("%w: task_id=%d action_hash mismatch", ErrInvalidInput, task.ID)
	}
	return nil
}

// executePropose runs the propose stage (every action except code_change) and
// routes the outcome: if the approval policy requires review, it parks at
// awaiting_approval with the proposal stored; otherwise the agent may finish
// the work in place.
func (e *AgentExecutor) executePropose(ctx context.Context, task *domain.Task, policy actionPolicy, execVersion int32) (*ExecuteResult, error) {
	run, propose, execErr := e.runPropose(ctx, task, policy)
	execErr = e.normalizeInterrupted(ctx, run, execErr)
	if writeErr := e.persistRun(ctx, run); writeErr != nil {
		return nil, fmt.Errorf("persist execution run task_id=%d: %w", task.ID, writeErr)
	}
	if execErr != nil {
		return e.finishRun(ctx, task, execVersion, run, execErr)
	}
	if propose.Outcome == "waiting" {
		return e.finishRun(ctx, task, execVersion, run, nil)
	}
	if propose.Outcome == "needs_human" && !propose.NeedsApproval {
		return e.finishRun(ctx, task, execVersion, run, nil)
	}
	if propose.NeedsApproval {
		proposalJSON, err := json.Marshal(proposalPayload(run, propose))
		if err != nil {
			return nil, fmt.Errorf("encode proposal task_id=%d: %w", task.ID, err)
		}
		if _, err := e.store.MarkAwaitingApproval(ctx, task.ID, execVersion, run.ID, proposalJSON); err != nil {
			return nil, fmt.Errorf("park Task id=%d awaiting approval: %w", task.ID, err)
		}
		return &ExecuteResult{
			TaskID: task.ID, RunID: run.ID, Status: "awaiting_approval",
			Summary: derefString(run.Summary),
		}, nil
	}
	// Pure read-only: codex already did the work; finish on its verdict.
	if propose.Outcome != "completed" {
		cause := fmt.Errorf("task not completed: %s", propose.FailureReason)
		return e.finishRun(ctx, task, execVersion, run, cause)
	}
	return e.finishRun(ctx, task, execVersion, run, nil)
}

// finishRun persists the terminal state of a run (done on success, failed on
// error), overwriting execution_result with the final verdict, and returns the
// summary result. It is the shared tail for code changes, pure read-only work,
// and the apply stage.
func (e *AgentExecutor) finishRun(ctx context.Context, task *domain.Task, execVersion int32, run *domain.ExecutionRun, execErr error) (*ExecuteResult, error) {
	ctx = context.WithoutCancel(ctx)
	if execErr == nil && run.Status == "waiting" {
		waiting, err := waitingFromRun(run)
		if err != nil {
			return nil, err
		}
		resultJSON, err := json.Marshal(runResultPayload(run, nil))
		if err != nil {
			return nil, fmt.Errorf("encode waiting result task_id=%d: %w", task.ID, err)
		}
		if _, err := e.store.MarkWaiting(ctx, task.ID, execVersion, run.ID, waiting.ScheduledTaskID, resultJSON); err != nil {
			return nil, fmt.Errorf("park Task id=%d waiting: %w", task.ID, err)
		}
		return &ExecuteResult{
			TaskID: task.ID, RunID: run.ID, Status: "waiting", Summary: derefString(run.Summary),
		}, nil
	}
	if execErr == nil && run.Status == "needs_human" {
		resultJSON, err := json.Marshal(runResultPayload(run, nil))
		if err != nil {
			return nil, fmt.Errorf("encode needs_human result task_id=%d: %w", task.ID, err)
		}
		if _, err := e.store.MarkNeedsHuman(ctx, task.ID, execVersion, run.ID, resultJSON); err != nil {
			return nil, fmt.Errorf("park Task id=%d needs_human: %w", task.ID, err)
		}
		return &ExecuteResult{
			TaskID: task.ID, RunID: run.ID, Status: "needs_human", Summary: derefString(run.Summary),
		}, nil
	}
	finishStatus := "done"
	if execErr != nil {
		finishStatus = "failed"
	}
	resultJSON, err := json.Marshal(runResultPayload(run, execErr))
	if err != nil {
		return nil, fmt.Errorf("encode execution result task_id=%d: %w", task.ID, err)
	}
	eventType := ""
	if errors.Is(execErr, ErrExecutionInterrupted) {
		eventType = "execution_interrupted"
	}
	if _, err := e.store.Finish(ctx, FinishInput{
		TaskID: task.ID, ExpectedVersion: execVersion, Status: finishStatus, Result: resultJSON,
		ActorType: "m5", RunID: &run.ID, EventType: eventType,
	}); err != nil {
		return nil, fmt.Errorf("finish Task id=%d after execution: %w", task.ID, err)
	}
	result := &ExecuteResult{
		TaskID: task.ID, RunID: run.ID, Status: finishStatus,
		Summary: derefString(run.Summary), Branch: derefString(run.Branch),
		Commit: derefString(run.Commit), DiffPath: derefString(run.DiffPath),
		MergeRequestURL: derefString(run.MergeRequestURL),
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
		TaskID: task.ID, ActionType: task.ActionType, Stage: "execute", Sandbox: policy.sandbox,
		Status: "running", StartedAt: startedAt,
	}

	var repoPath string
	var repo *gitRepo
	baseBranch := ""
	if task.ActionType == "code_change" {
		// Resolve the working directory from the M3-frozen context (repo_ref slot
		// first, then project.repos[].local_path). repos being empty must NOT block
		// this round (docs/design-context-pipeline.md §7): codex runs without --cd
		// and self-locates / reports the missing repo.
		path, err := e.resolveRepo(task)
		if err != nil {
			return e.failRun(run, startedAt, err), err
		}
		if path != "" {
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
			run.BaseBranch = &baseBranch
			branch := fmt.Sprintf("jarvis/task-%d", task.ID)
			if err := repo.createBranch(ctx, branch); err != nil {
				return e.failRun(run, startedAt, err), err
			}
			run.Branch = &branch
		}
	}

	previousRuns, err := e.loadPriorRunSummaries(ctx, task.ID)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	sharedMemory, err := e.sharedMem.Text(ctx)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	workRules, err := e.workRules.Block(ctx, workrule.StageExecute)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	skills, err := e.skills.Catalog(ctx, skill.StageExecute)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	systemPrompt, err := e.textStore.Content(ctx, textstore.SystemPromptM5Key)
	if err != nil {
		cause := fmt.Errorf("load M5 execution system prompt: %w", err)
		return e.failRun(run, startedAt, cause), cause
	}
	toolCatalog, err := toolcatalog.Block(toolcatalog.StageExecute)
	if err != nil {
		cause := fmt.Errorf("load M5 tool catalog: %w", err)
		return e.failRun(run, startedAt, cause), cause
	}
	prompt, err := buildExecutionPrompt(systemPrompt, task, repoPath, toolCatalog, sharedMemory, workRules, skills, previousRuns)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	run.Prompt = prompt

	outputCapture, err := e.prepareTaskRunOutput(task.ID, run.Stage, startedAt, prompt)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	codexOut, err := e.runner.RunTaskWithOutput(ctx, prompt, policy.sandbox, repoPath, schemaExecution, task.ID, outputCapture)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	run.CodexSessionID = &codexOut.SessionID

	// codex reports its own success verdict (executionResultSchema). A run that
	// finished the process but did not achieve the goal (e.g. "message not
	// sent") is a failure — surface it instead of silently marking succeeded.
	if codexOut.Result == nil {
		cause := fmt.Errorf("codex exec returned no structured result")
		run.Summary = &codexOut.LastMessage
		return e.failRun(run, startedAt, cause), cause
	}
	// Store the clean human summary on Summary and the full structured verdict
	// (success/failure_reason/needs_followup/enrichments) on Output, so the UI
	// shows prose, not a raw JSON blob.
	summary := codexOut.Result.Summary
	run.Summary = &summary
	if structured, err := json.Marshal(codexOut.Result); err == nil {
		run.Output = structured
	}
	if codexOut.Result.Outcome == "waiting" {
		finished := e.now().UTC()
		run.Status = "waiting"
		run.FinishedAt = &finished
		ms := finished.Sub(startedAt).Milliseconds()
		run.DurationMs = &ms
		return run, nil
	}
	if codexOut.Result.Outcome == "needs_human" {
		e.finishNeedsHumanRun(run, startedAt)
		return run, nil
	}
	if codexOut.Result.Outcome != "completed" {
		cause := fmt.Errorf("task not completed: %s", codexOut.Result.FailureReason)
		return e.failRun(run, startedAt, cause), cause
	}

	if repo != nil {
		if err := e.finalizeCodeChange(ctx, task, run, repo, baseBranch); err != nil {
			return e.failRun(run, startedAt, err), err
		}
	}

	finished := e.now().UTC()
	run.Status = "succeeded"
	run.FinishedAt = &finished
	ms := finished.Sub(startedAt).Milliseconds()
	run.DurationMs = &ms
	return run, nil
}

func (e *AgentExecutor) finalizeCodeChange(ctx context.Context, task *domain.Task, run *domain.ExecutionRun, repo *gitRepo, baseBranch string) error {
	if task == nil || run == nil || repo == nil || run.Branch == nil || strings.TrimSpace(*run.Branch) == "" || strings.TrimSpace(baseBranch) == "" {
		return fmt.Errorf("%w: incomplete Git delivery state for code_change", ErrInvalidInput)
	}
	changed, err := repo.hasChanges(ctx)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	commit, err := repo.commitAll(ctx, fmt.Sprintf("jarvis: task %d — %s", task.ID, task.Title))
	if err != nil {
		return err
	}
	run.Commit = &commit
	diff, err := repo.diffAgainst(ctx, baseBranch)
	if err != nil {
		return err
	}
	diffPath, err := e.writeDiff(task.ID, diff)
	if err != nil {
		return err
	}
	run.DiffPath = &diffPath
	// Full-autonomy: push the branch and open an MR. A push/MR failure is a
	// real failure of a code_change Task (the change never leaves the box).
	pushOut, err := repo.pushBranchWithMR(ctx, *run.Branch, baseBranch)
	if err != nil {
		return err
	}
	if url := extractMergeRequestURL(pushOut); url != "" {
		run.MergeRequestURL = &url
	}
	return nil
}

// runPropose runs the propose stage. It asks codex to judge — by what it will
// actually do — whether the run touches the outside world, and to either finish
// read-only/local work or produce a proposal WITHOUT touching the outside world
// (see buildProposePrompt). The propose stage never runs for code_change, so
// there is no repo/git handling here. It always returns a populated
// *domain.ExecutionRun; execErr is non-nil on any failure.
func (e *AgentExecutor) runPropose(ctx context.Context, task *domain.Task, policy actionPolicy) (*domain.ExecutionRun, *proposeResult, error) {
	startedAt := e.now().UTC()
	run := &domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Stage: "propose", Sandbox: policy.sandbox,
		Status: "running", StartedAt: startedAt,
	}

	previousRuns, err := e.loadPriorRunSummaries(ctx, task.ID)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	sharedMemory, err := e.sharedMem.Text(ctx)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	workRules, err := e.workRules.Block(ctx, workrule.StageExecute)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	skills, err := e.skills.Catalog(ctx, skill.StageExecute)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	systemPrompt, err := e.textStore.Content(ctx, textstore.SystemPromptM5Key)
	if err != nil {
		cause := fmt.Errorf("load M5 propose system prompt: %w", err)
		return e.failRun(run, startedAt, cause), nil, cause
	}
	approvalPolicy, err := e.textStore.Content(ctx, textstore.ApprovalPolicyKey)
	if err != nil {
		cause := fmt.Errorf("load M5 approval policy: %w", err)
		return e.failRun(run, startedAt, cause), nil, cause
	}
	toolCatalog, err := toolcatalog.Block(toolcatalog.StageExecute)
	if err != nil {
		cause := fmt.Errorf("load M5 tool catalog: %w", err)
		return e.failRun(run, startedAt, cause), nil, cause
	}
	prompt, err := buildProposePrompt(systemPrompt, approvalPolicy, task, toolCatalog, sharedMemory, workRules, skills, previousRuns)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	run.Prompt = prompt

	outputCapture, err := e.prepareTaskRunOutput(task.ID, run.Stage, startedAt, prompt)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	codexOut, err := e.runner.RunTaskWithOutput(ctx, prompt, policy.sandbox, "", schemaPropose, task.ID, outputCapture)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	run.CodexSessionID = &codexOut.SessionID
	if codexOut.Propose == nil {
		cause := fmt.Errorf("codex propose returned no structured result")
		run.Summary = &codexOut.LastMessage
		return e.failRun(run, startedAt, cause), nil, cause
	}
	propose := codexOut.Propose
	summary := propose.Summary
	run.Summary = &summary
	if structured, err := json.Marshal(propose); err == nil {
		run.Output = structured
	}

	finished := e.now().UTC()
	run.Status = "succeeded"
	if propose.Outcome == "waiting" {
		run.Status = "waiting"
	} else if propose.Outcome == "needs_human" && !propose.NeedsApproval {
		run.Status = "needs_human"
	}
	run.FinishedAt = &finished
	ms := finished.Sub(startedAt).Milliseconds()
	run.DurationMs = &ms
	return run, propose, nil
}

// runApply lands an approved proposal. It builds a fresh codex invocation with
// the approved plan + artifact embedded (buildApplyPrompt) and runs it under the
// normal executionResultSchema so the real external write reports a real success
// verdict. Like runPropose, external actions carry no repo/git handling.
func (e *AgentExecutor) runApply(ctx context.Context, task *domain.Task, policy actionPolicy, proposal *codexProposal) (*domain.ExecutionRun, error) {
	startedAt := e.now().UTC()
	run := &domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Stage: "apply", Sandbox: policy.sandbox,
		Status: "running", StartedAt: startedAt,
	}

	previousRuns, err := e.loadPriorRunSummaries(ctx, task.ID)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	sharedMemory, err := e.sharedMem.Text(ctx)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	workRules, err := e.workRules.Block(ctx, workrule.StageExecute)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	systemPrompt, err := e.textStore.Content(ctx, textstore.SystemPromptM5Key)
	if err != nil {
		cause := fmt.Errorf("load M5 apply system prompt: %w", err)
		return e.failRun(run, startedAt, cause), cause
	}
	toolCatalog, err := toolcatalog.Block(toolcatalog.StageExecute)
	if err != nil {
		cause := fmt.Errorf("load M5 tool catalog: %w", err)
		return e.failRun(run, startedAt, cause), cause
	}
	skills, err := e.skills.Catalog(ctx, skill.StageExecute)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	prompt, err := buildApplyPrompt(systemPrompt, task, proposal, toolCatalog, sharedMemory, workRules, skills, previousRuns)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	run.Prompt = prompt

	outputCapture, err := e.prepareTaskRunOutput(task.ID, run.Stage, startedAt, prompt)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	codexOut, err := e.runner.RunTaskWithOutput(ctx, prompt, policy.sandbox, "", schemaExecution, task.ID, outputCapture)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	run.CodexSessionID = &codexOut.SessionID
	if codexOut.Result == nil {
		cause := fmt.Errorf("codex exec returned no structured result")
		run.Summary = &codexOut.LastMessage
		return e.failRun(run, startedAt, cause), cause
	}
	summary := codexOut.Result.Summary
	run.Summary = &summary
	if structured, err := json.Marshal(codexOut.Result); err == nil {
		run.Output = structured
	}
	if codexOut.Result.Outcome == "waiting" {
		finished := e.now().UTC()
		run.Status = "waiting"
		run.FinishedAt = &finished
		ms := finished.Sub(startedAt).Milliseconds()
		run.DurationMs = &ms
		return run, nil
	}
	if codexOut.Result.Outcome == "needs_human" {
		e.finishNeedsHumanRun(run, startedAt)
		return run, nil
	}
	if codexOut.Result.Outcome != "completed" {
		cause := fmt.Errorf("task not completed: %s", codexOut.Result.FailureReason)
		return e.failRun(run, startedAt, cause), cause
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

func (e *AgentExecutor) normalizeInterrupted(ctx context.Context, run *domain.ExecutionRun, execErr error) error {
	if !errors.Is(context.Cause(ctx), ErrExecutionInterrupted) {
		return execErr
	}
	if run != nil {
		e.failRun(run, run.StartedAt, ErrExecutionInterrupted)
	}
	return ErrExecutionInterrupted
}

func (e *AgentExecutor) persistRun(ctx context.Context, run *domain.ExecutionRun) error {
	return e.db.WithContext(context.WithoutCancel(ctx)).Create(run).Error
}

func waitingFromRun(run *domain.ExecutionRun) (*codexWaiting, error) {
	if run == nil || run.Status != "waiting" || len(run.Output) == 0 {
		return nil, fmt.Errorf("execution run is not waiting")
	}
	var output struct {
		Waiting *codexWaiting `json:"waiting"`
	}
	if err := json.Unmarshal(run.Output, &output); err != nil {
		return nil, fmt.Errorf("decode waiting execution output: %w", err)
	}
	if output.Waiting == nil || output.Waiting.ScheduledTaskID == 0 {
		return nil, fmt.Errorf("waiting execution output has no scheduled task")
	}
	return output.Waiting, nil
}

// resolveRepo locates the local git repo for a code_change Task from the
// M3-frozen context. It reads the snapshot's project.repos[].local_path.
// Returns ("", nil) when no repo is available so the caller runs codex without
// --cd (repos empty must not block — see docs/design-context-pipeline.md §7).
func (e *AgentExecutor) resolveRepo(task *domain.Task) (string, error) {
	// A malformed snapshot must surface; an absent/empty repos list is non-blocking.
	for _, localPath := range snapshotRepoLocalPaths(task.Background) {
		path := e.absRepoPath(localPath)
		if isGitDir(path) {
			return path, nil
		}
	}
	return "", nil
}

// absRepoPath honors an absolute path as-is; otherwise it joins under repoRoot.
func (e *AgentExecutor) absRepoPath(ref string) string {
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(e.repoRoot, ref)
}

func isGitDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

// snapshotRepoLocalPaths extracts non-empty local_path values from the frozen
// context_snapshot's project.repos ([{name,url,local_path}]). It is lenient on
// absent/null repos (returns nothing) but ignores structurally broken repos.
func snapshotRepoLocalPaths(background []byte) []string {
	snapshot, err := contextsnap.Decode(background)
	if err != nil || snapshot.Project == nil || len(snapshot.Project.Repos) == 0 {
		return nil
	}
	var repos []struct {
		LocalPath string `json:"local_path"`
	}
	if err := json.Unmarshal(snapshot.Project.Repos, &repos); err != nil {
		return nil
	}
	paths := make([]string, 0, len(repos))
	for _, repo := range repos {
		if p := strings.TrimSpace(repo.LocalPath); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
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
	// stage tags where this terminal result came from so the UI can tell a real
	// codex execution failure (stage=executed + error) apart from a human
	// rejection (stage=rejected) or manual mark-failed (stage=manual_failed).
	stage := "executed"
	if errors.Is(execErr, ErrExecutionInterrupted) {
		stage = "interrupted"
	}
	payload := map[string]any{
		"stage":         stage,
		"action_type":   run.ActionType,
		"sandbox":       run.Sandbox,
		"run_status":    run.Status,
		"source_run_id": run.ID,
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
	if run.MergeRequestURL != nil {
		payload["merge_request_url"] = *run.MergeRequestURL
	}
	if run.CodexSessionID != nil {
		payload["codex_session_id"] = *run.CodexSessionID
	}
	// Surface the assistant's structured verdict (needs_followup + the "多做一步"
	// enrichments) so the UI can show them separately from the prose summary.
	if len(run.Output) > 0 {
		var structured codexResult
		if err := json.Unmarshal(run.Output, &structured); err == nil {
			payload["outcome"] = structured.Outcome
			if strings.TrimSpace(structured.NeedsFollowup) != "" {
				payload["needs_followup"] = structured.NeedsFollowup
			}
			if len(structured.Enrichments) > 0 {
				payload["enrichments"] = structured.Enrichments
			}
			if structured.Waiting != nil {
				payload["waiting"] = structured.Waiting
			}
		}
	}
	if execErr != nil {
		payload["error"] = execErr.Error()
	}
	return payload
}

func resultHasStage(raw []byte, stage string) bool {
	var payload struct {
		Stage string `json:"stage"`
	}
	return json.Unmarshal(raw, &payload) == nil && payload.Stage == stage
}

// proposalPayload builds the execution_result stored while a Task waits at
// awaiting_approval. stage="proposal" marks it so the UI and apply stage can
// tell a pending proposal apart from a final run result. It carries the full
// artifact so the human reviews exactly what will be written.
func proposalPayload(run *domain.ExecutionRun, propose *proposeResult) map[string]any {
	payload := map[string]any{
		"stage":       "proposal",
		"action_type": run.ActionType,
		"summary":     propose.Summary,
		"proposal": map[string]any{
			"action":   propose.Proposal.Action,
			"target":   propose.Proposal.Target,
			"artifact": propose.Proposal.Artifact,
		},
	}
	if strings.TrimSpace(propose.NeedsFollowup) != "" {
		payload["needs_followup"] = propose.NeedsFollowup
	}
	if len(propose.Enrichments) > 0 {
		payload["enrichments"] = propose.Enrichments
	}
	if run.CodexSessionID != nil {
		payload["codex_session_id"] = *run.CodexSessionID
	}
	return payload
}

// rejectionPayload builds the execution_result stored when a human rejects a
// proposal (Task -> failed). stage="rejected" distinguishes it from an execution
// failure so the UI can show "you declined this" rather than "codex failed".
func rejectionPayload(reason string) map[string]any {
	payload := map[string]any{"stage": "rejected", "summary": "委托人驳回了外部写入方案"}
	if reason != "" {
		payload["reject_reason"] = reason
	}
	return payload
}

// decodeStoredProposal reads the proposal that MarkAwaitingApproval stored in
// execution_result. It fails-fast if the stored payload is not a proposal or is
// missing the artifact — the apply stage must have a real artifact to land.
func decodeStoredProposal(raw []byte) (*codexProposal, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("execution_result is empty, no proposal to apply")
	}
	var stored struct {
		Stage    string         `json:"stage"`
		Proposal *codexProposal `json:"proposal"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode stored proposal: %w", err)
	}
	if stored.Stage != "proposal" || stored.Proposal == nil {
		return nil, fmt.Errorf("stored execution_result is not a pending proposal (stage=%q)", stored.Stage)
	}
	if strings.TrimSpace(stored.Proposal.Action) == "" ||
		strings.TrimSpace(stored.Proposal.Target) == "" ||
		strings.TrimSpace(stored.Proposal.Artifact) == "" {
		return nil, fmt.Errorf("stored proposal is missing action, target or artifact")
	}
	return stored.Proposal, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// extractMergeRequestURL scans git remote push output for the MR/PR URL that
// GitLab/ByteDance-style remotes print after a push. It looks for the first
// http(s) URL on a line mentioning a merge/pull request. Returns "" when the
// remote printed no such URL (push options ignored) — that is non-fatal.
func extractMergeRequestURL(pushOutput string) string {
	for _, line := range strings.Split(pushOutput, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "merge request") && !strings.Contains(lower, "pull request") && !strings.Contains(lower, "/merge_requests/") {
			continue
		}
		if idx := strings.Index(line, "http"); idx >= 0 {
			return strings.TrimSpace(line[idx:])
		}
	}
	return ""
}
