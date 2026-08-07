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

	"jarvis/internal/agentusage"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
	"jarvis/internal/observability"
	"jarvis/internal/prompttemplate"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
	"jarvis/internal/workrule"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

var (
	ErrExecutionInterrupted = errors.New("execution interrupted by user")
)

const executionSandbox = "danger-full-access"

// ExecuteInput drives one Task execution.
type ExecuteInput struct {
	TaskID uint64
}

// ExecuteResult summarizes what happened, for the API/log. When Status is
// awaiting_approval, codex identified a required mutation and produced a
// proposal without performing it; the Task is parked for human approval.
type ExecuteResult struct {
	TaskID  uint64 `json:"task_id"`
	RunID   uint64 `json:"run_id"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

// ApprovalNotification is the durable proposal projected into a user-facing
// card only after Task.status has become awaiting_approval.
type ApprovalNotification struct {
	TaskID   uint64
	RunID    uint64
	Version  int32
	Title    string
	Summary  string
	Action   string
	Target   string
	Artifact string
}

type ApprovalDelivery struct {
	MessageID string
	Target    string
	Preview   string
	URL       string
}

type ApprovalNotifier interface {
	SendApproval(context.Context, ApprovalNotification) (*ApprovalDelivery, error)
}

// AgentExecutor is the execution core. It does not hard-code a per-action
// workflow: it hands the Task's source payload/background and any resolved repo
// path to codex and lets codex orchestrate.
// Whether a side effect needs human approval, and how code is delivered, are
// the model's judgment — declared via needs_approval/proposal and effects.
type AgentExecutor struct {
	store     *Store
	runner    *CodexRunner
	sharedMem sharedmem.SharedMemoryReader
	workRules workrule.Reader
	textStore textstore.Reader
	skills    skill.Reader
	approvals ApprovalNotifier
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

func NewAgentExecutor(store *Store, runner *CodexRunner, sharedMem sharedmem.SharedMemoryReader, workRules workrule.Reader, textStore textstore.Reader, skills skill.Reader, approvals ApprovalNotifier, repoRoot, runsDir string) (*AgentExecutor, error) {
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
		store: store, runner: runner, sharedMem: sharedMem, workRules: workRules, textStore: textStore, skills: skills, approvals: approvals,
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

func (e *AgentExecutor) runInBackground(taskID uint64, runCtx context.Context, active *activeExecution, run func(context.Context) (*ExecuteResult, error)) {
	go func() {
		ended := false
		defer func() {
			if !ended {
				e.endExecution(taskID, active)
			}
		}()
		result, err := run(runCtx)
		// Release the old run before publishing its approval card. A click can
		// then claim the same Task immediately instead of racing the active slot.
		e.endExecution(taskID, active)
		ended = true
		if err != nil {
			hlog.CtxErrorf(runCtx, "background execution failed task_id=%d error=%+v", taskID, err)
			return
		}
		if err := e.notifyAwaitingApproval(context.WithoutCancel(runCtx), result); err != nil {
			hlog.CtxErrorf(runCtx, "approval notification failed task_id=%d error=%+v", taskID, err)
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
	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load Task before interrupt: %w", err)
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

	finished, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
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
	task, err := e.store.LoadTask(ctx, input.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Status != "pending" {
		return nil, fmt.Errorf("%w: task_id=%d from=%s to=executing", ErrInvalidTransition, task.ID, task.Status)
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
// stage previously failed (failed -> executing), WITHOUT restarting the initial
// execution and asking for approval again. It recovers the last approved
// proposal from the run history,
// claims the Task, and runs the apply stage in the background. It fails-fast if
// no approved proposal is recoverable (the Task never went through approval — the
// caller should use rerun instead). Poll Task status for completion.
func (e *AgentExecutor) KickReapply(ctx context.Context, taskID uint64) (*ExecuteResult, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != "failed" {
		return nil, fmt.Errorf("%w: task_id=%d status=%s cannot re-apply (only failed apply attempts)", ErrInvalidTransition, task.ID, task.Status)
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
	e.runInBackground(task.ID, runCtx, active, func(runCtx context.Context) (*ExecuteResult, error) {
		return e.applyApproved(runCtx, task, proposal, execVersion)
	})
	return &ExecuteResult{TaskID: task.ID, Status: "executing"}, nil
}

func (e *AgentExecutor) executeClaimedInBackground(runCtx context.Context, active *activeExecution, taskID uint64, execVersion int32) {
	e.runInBackground(taskID, runCtx, active, func(runCtx context.Context) (*ExecuteResult, error) {
		return e.executeClaimed(runCtx, taskID, execVersion)
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
	approvalPolicy, err := e.textStore.Content(ctx, textstore.ApprovalPolicyKey)
	if err != nil {
		return fmt.Errorf("load M5 approval policy for waiting resume: %w", err)
	}
	prompt, err := buildScheduledResumePrompt(systemPrompt, approvalPolicy, reason, workRules, toolCatalog)
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
	e.runInBackground(taskID, runCtx, active, func(runCtx context.Context) (*ExecuteResult, error) {
		return e.resumeClaimed(runCtx, taskID, sourceRunID, prompt, execVersion)
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
	approvalPolicy, err := e.textStore.Content(ctx, textstore.ApprovalPolicyKey)
	if err != nil {
		return nil, fmt.Errorf("load M5 approval policy for human resume: %w", err)
	}
	prompt, err := buildHumanResumePrompt(systemPrompt, approvalPolicy, response, workRules, toolCatalog)
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
	e.runInBackground(taskID, runCtx, active, func(runCtx context.Context) (*ExecuteResult, error) {
		return e.resumeClaimed(runCtx, claim.TaskID, claim.SourceRunID, prompt, claim.Version)
	})
	return &ExecuteResult{TaskID: claim.TaskID, Status: "executing"}, nil
}

func (e *AgentExecutor) resumeClaimed(ctx context.Context, taskID, sourceRunID uint64, prompt string, execVersion int32) (*ExecuteResult, error) {
	task, err := e.store.LoadTask(context.WithoutCancel(ctx), taskID)
	if err != nil {
		return nil, fmt.Errorf("load resumed Task id=%d: %w", taskID, err)
	}
	source, err := e.store.LoadRun(context.WithoutCancel(ctx), sourceRunID)
	if err != nil {
		return nil, fmt.Errorf("load resumed source run id=%d: %w", sourceRunID, err)
	}
	if source.TaskID != task.ID || source.CodexSessionID == nil || strings.TrimSpace(*source.CodexSessionID) == "" {
		return nil, fmt.Errorf("%w: source_run_id=%d has no persisted Codex session for task_id=%d", ErrInvalidInput, sourceRunID, taskID)
	}
	stage := source.Stage
	if stage == "" {
		stage = "execute"
	}
	startedAt := e.now().UTC()
	run := &domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Stage: stage, Sandbox: executionSandbox,
		Status: "running", Prompt: prompt, StartedAt: startedAt,
	}
	if err := e.markRunStarted(ctx, run); err != nil {
		return nil, err
	}
	if errors.Is(context.Cause(ctx), ErrExecutionInterrupted) {
		e.failRun(run, startedAt, ErrExecutionInterrupted)
		if writeErr := e.persistRun(ctx, run); writeErr != nil {
			return nil, fmt.Errorf("persist interrupted resumed run task_id=%d: %w", task.ID, writeErr)
		}
		return e.finishRun(ctx, task, execVersion, run, ErrExecutionInterrupted)
	}
	repoPath := ""
	if source.RepoPath != nil {
		repoPath = strings.TrimSpace(*source.RepoPath)
		run.RepoPath = &repoPath
	}
	outputCapture, err := e.prepareTaskRunOutput(task.ID, stage, startedAt, prompt)
	if err != nil {
		e.failRun(run, startedAt, err)
		if writeErr := e.persistRun(ctx, run); writeErr != nil {
			return nil, fmt.Errorf("persist failed resumed run task_id=%d: %w", task.ID, writeErr)
		}
		return e.finishRun(ctx, task, execVersion, run, err)
	}
	codexOut, execErr := e.runner.ResumeTaskWithOutput(ctx, *source.CodexSessionID, prompt, executionSandbox, repoPath, schemaExecution, task.ID, outputCapture)
	if codexOut != nil {
		recordAgentUsage(run, codexOut.Usage)
	}
	if execErr != nil {
		e.failRun(run, startedAt, execErr)
		if writeErr := e.persistRun(ctx, run); writeErr != nil {
			return nil, fmt.Errorf("persist failed resumed run task_id=%d: %w", task.ID, writeErr)
		}
		return e.finishRun(ctx, task, execVersion, run, execErr)
	}
	run.CodexSessionID = &codexOut.SessionID
	if codexOut.Result == nil {
		execErr = fmt.Errorf("resumed codex exec returned no structured result")
		e.failRun(run, startedAt, execErr)
		if writeErr := e.persistRun(ctx, run); writeErr != nil {
			return nil, fmt.Errorf("persist resumed run task_id=%d: %w", task.ID, writeErr)
		}
		return e.finishRun(ctx, task, execVersion, run, execErr)
	}
	result := codexOut.Result
	if err := recordAgentVerdict(run, result.Summary, result, result.Effects); err != nil {
		return nil, fmt.Errorf("record resumed execution verdict task_id=%d: %w", task.ID, err)
	}
	if result.Outcome == "waiting" {
		e.finishWaitingRun(run, startedAt)
	} else if result.NeedsApproval {
		e.finishSuccessfulRun(run, startedAt)
	} else if result.Outcome == "needs_human" {
		e.finishNeedsHumanRun(run, startedAt)
	} else if result.Outcome == "observing" {
		e.finishObservingRun(run, startedAt)
	} else if result.Outcome == "completed" {
		e.finishSuccessfulRun(run, startedAt)
	} else {
		execErr = fmt.Errorf("task not completed: %s", result.FailureReason)
		e.failRun(run, startedAt, execErr)
	}
	if writeErr := e.persistRun(ctx, run); writeErr != nil {
		return nil, fmt.Errorf("persist resumed run task_id=%d: %w", task.ID, writeErr)
	}
	if execErr != nil {
		return e.finishRun(ctx, task, execVersion, run, execErr)
	}
	if result.NeedsApproval {
		proposalJSON, err := json.Marshal(proposalPayload(run, result))
		if err != nil {
			return nil, fmt.Errorf("encode resumed proposal task_id=%d: %w", task.ID, err)
		}
		e.recordRunProgress(ctx, task, run)
		if _, err := e.store.MarkAwaitingApproval(ctx, task.ID, execVersion, run.ID, proposalJSON); err != nil {
			return nil, err
		}
		return &ExecuteResult{TaskID: task.ID, RunID: run.ID, Status: "awaiting_approval", Summary: result.Summary}, nil
	}
	return e.finishRun(ctx, task, execVersion, run, nil)
}

// buildScheduledResumePrompt builds the prompt for a woken-up waiting Task. It
// carries the approval policy because a resumed session runs under the same
// result contract as a first pass: it may well decide the next step needs
// approval, and it cannot make that call without the policy text.
func buildScheduledResumePrompt(systemPrompt, approvalPolicy, reason, workRules, toolCatalog string) (string, error) {
	systemPrompt = strings.TrimSpace(systemPrompt)
	reason = strings.TrimSpace(reason)
	if systemPrompt == "" || reason == "" {
		return "", fmt.Errorf("waiting resume system prompt and reason are required")
	}
	prompt, err := renderResumeInstructions(systemPrompt, approvalPolicy, m5PhaseResumeWaiting, workRules, toolCatalog)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n\n等待原因：%s\n当前时间：%s", prompt, reason, time.Now().UTC().Format(time.RFC3339)), nil
}

// buildHumanResumePrompt builds the prompt that continues a session after the
// principal answered. It carries the approval policy for the same reason as the
// waiting resume above.
func buildHumanResumePrompt(systemPrompt, approvalPolicy, response, workRules, toolCatalog string) (string, error) {
	systemPrompt = strings.TrimSpace(systemPrompt)
	response = strings.TrimSpace(response)
	if systemPrompt == "" || response == "" {
		return "", fmt.Errorf("human resume system prompt and response are required")
	}
	prompt, err := renderResumeInstructions(systemPrompt, approvalPolicy, m5PhaseResumeHuman, workRules, toolCatalog)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n\n委托人回应：%s\n当前时间：%s", prompt, response, time.Now().UTC().Format(time.RFC3339)), nil
}

// renderResumeInstructions assembles the shared resume preamble: phase, approval
// policy, work rules and tool catalog.
func renderResumeInstructions(systemPrompt, approvalPolicy, phase, workRules, toolCatalog string) (string, error) {
	renderedSystemPrompt, err := prompttemplate.Render(prompttemplate.StageM5, systemPrompt, workRules, approvalPolicy)
	if err != nil {
		return "", fmt.Errorf("render M5 resume system prompt: %w", err)
	}
	prompt := renderedSystemPrompt + "\n\n" + phase
	if block := strings.TrimSpace(toolCatalog); block != "" {
		prompt += "\n\n" + block
	}
	return prompt, nil
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

// finishObservingRun records a run that investigated properly and concluded
// nobody needs to act. The run did its job, so this is not a failure; it just
// did not have to change anything, so it is not a completion either.
func (e *AgentExecutor) finishObservingRun(run *domain.ExecutionRun, startedAt time.Time) {
	finished := e.now().UTC()
	run.Status = "observing"
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
	task, proposal, execVersion, err := e.claimForApproval(ctx, taskID, expectedVersion)
	if err != nil {
		e.abandonExecution(taskID, active)
		return nil, err
	}
	e.runInBackground(taskID, runCtx, active, func(runCtx context.Context) (*ExecuteResult, error) {
		return e.applyApproved(runCtx, task, proposal, execVersion)
	})
	return &ExecuteResult{TaskID: task.ID, Status: "executing"}, nil
}

// Approve lands a proposal that a human accepted, synchronously. It claims the
// awaiting_approval Task (-> executing), rebuilds a fresh codex invocation with
// the approved proposal embedded in the prompt (the apply stage — codex exec
// --ephemeral cannot resume the initial execution session, so this is a new run that
// faithfully lands the already-decided artifact), and finishes the Task
// done/failed on the real external write's verdict. Prefer KickApprove from HTTP
// handlers; this stays for callers that need to block on the outcome (tests).
func (e *AgentExecutor) Approve(ctx context.Context, taskID uint64, expectedVersion int32) (*ExecuteResult, error) {
	task, proposal, execVersion, err := e.claimForApproval(ctx, taskID, expectedVersion)
	if err != nil {
		return nil, err
	}
	result, err := e.applyApproved(ctx, task, proposal, execVersion)
	if err != nil {
		return nil, err
	}
	if err := e.notifyAwaitingApproval(context.WithoutCancel(ctx), result); err != nil {
		return nil, err
	}
	return result, nil
}

// claimForApproval validates an awaiting_approval Task, decodes its stored
// proposal, and atomically claims it (awaiting_approval -> executing) under
// optimistic lock. It is the synchronous prefix shared by Approve and
// KickApprove so version/state conflicts fail fast before any codex work starts.
func (e *AgentExecutor) claimForApproval(ctx context.Context, taskID uint64, expectedVersion int32) (*domain.Task, *codexProposal, int32, error) {
	if taskID == 0 {
		return nil, nil, 0, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, nil, 0, err
	}
	if task.Version != expectedVersion {
		return nil, nil, 0, fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, expectedVersion, task.Version)
	}
	if task.Status != "awaiting_approval" {
		return nil, nil, 0, fmt.Errorf("%w: task_id=%d status=%s cannot be approved", ErrInvalidTransition, task.ID, task.Status)
	}
	proposal, err := decodeStoredProposal(task.ExecutionResult)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("read stored proposal task_id=%d: %w", task.ID, err)
	}
	execVersion, err := e.store.MarkExecutingFromApproval(ctx, task.ID, task.Version)
	if err != nil {
		return nil, nil, 0, err
	}
	return task, proposal, execVersion, nil
}

// applyApproved runs the apply stage for an already-claimed Task and finishes it
// done/failed on the real external write's verdict. It is the shared tail of
// Approve (synchronous) and KickApprove (background goroutine).
func (e *AgentExecutor) applyApproved(ctx context.Context, task *domain.Task, proposal *codexProposal, execVersion int32) (*ExecuteResult, error) {
	run, result, execErr := e.runApply(ctx, task, proposal)
	return e.routeRun(ctx, task, execVersion, run, result, execErr)
}

// Reject declines a proposed external write. It moves the awaiting_approval Task
// to failed and records the rejection (optionally with a reason) in
// execution_result so the UI shows why; the Task can later be rerun to produce a
// new proposal when needed.
func (e *AgentExecutor) Reject(ctx context.Context, taskID uint64, expectedVersion int32, reason string) (*ExecuteResult, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: task_id must be positive", ErrInvalidInput)
	}
	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, err
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
	task, err := e.store.LoadTask(ctx, input.TaskID)
	if err != nil {
		return nil, err
	}

	runCtx, active, err := e.beginExecution(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	ended := false
	defer func() {
		if !ended {
			e.endExecution(task.ID, active)
		}
	}()

	// Claim the Task (pending -> executing). This is the concurrency guard.
	execVersion, err := e.store.MarkExecuting(ctx, task.ID, task.Version)
	if err != nil {
		return nil, err
	}
	result, err := e.executeClaimed(runCtx, task.ID, execVersion)
	e.endExecution(task.ID, active)
	ended = true
	if err != nil {
		return nil, err
	}
	if err := e.notifyAwaitingApproval(context.WithoutCancel(ctx), result); err != nil {
		return nil, err
	}
	return result, nil
}

// executeClaimed runs an already-claimed (executing) Task. Used by KickExecute /
// KickRerun after a synchronous claim, and by Execute after MarkExecuting.
func (e *AgentExecutor) executeClaimed(ctx context.Context, taskID uint64, execVersion int32) (*ExecuteResult, error) {
	task, err := e.store.LoadTask(context.WithoutCancel(ctx), taskID)
	if err != nil {
		return nil, err
	}
	// Every action_type takes the same path. Whether the side effect this run is
	// about to cause needs human review, and how any code change is delivered,
	// are the agent's judgment under the file-backed approval policy.
	return e.executeRun(ctx, task, execVersion)
}

// executeRun runs a Task's first pass and routes the outcome.
func (e *AgentExecutor) executeRun(ctx context.Context, task *domain.Task, execVersion int32) (*ExecuteResult, error) {
	run, result, execErr := e.runOnce(ctx, task)
	return e.routeRun(ctx, task, execVersion, run, result, execErr)
}

// routeRun persists a finished run and sends the Task where the agent's verdict
// says it should go: parked at awaiting_approval when the agent stopped to ask,
// otherwise on to its terminal state.
//
// Every stage shares this tail, including apply. An approved proposal can turn
// up a *second* side effect that the policy also gates, and that request has to
// be able to stop the Task again — otherwise the agent's proposal is silently
// dropped and the approve button has nothing to act on.
func (e *AgentExecutor) routeRun(ctx context.Context, task *domain.Task, execVersion int32, run *domain.ExecutionRun, result *codexResult, execErr error) (*ExecuteResult, error) {
	execErr = e.normalizeInterrupted(ctx, run, execErr)
	if writeErr := e.persistRun(ctx, run); writeErr != nil {
		return nil, fmt.Errorf("persist execution run task_id=%d: %w", task.ID, writeErr)
	}
	if execErr != nil {
		return e.finishRun(ctx, task, execVersion, run, execErr)
	}
	if result != nil && result.NeedsApproval {
		proposalJSON, err := json.Marshal(proposalPayload(run, result))
		if err != nil {
			return nil, fmt.Errorf("encode proposal task_id=%d: %w", task.ID, err)
		}
		// Parking for approval does not pass through finishRun, but the
		// investigation that produced the proposal is real progress.
		e.recordRunProgress(ctx, task, run)
		if _, err := e.store.MarkAwaitingApproval(ctx, task.ID, execVersion, run.ID, proposalJSON); err != nil {
			return nil, fmt.Errorf("park Task id=%d awaiting approval: %w", task.ID, err)
		}
		return &ExecuteResult{
			TaskID: task.ID, RunID: run.ID, Status: "awaiting_approval",
			Summary: derefString(run.Summary),
		}, nil
	}
	return e.finishRun(ctx, task, execVersion, run, nil)
}

// notifyAwaitingApproval projects an already-persisted proposal into Feishu.
// Persistence is the truth; notification is a subsequent side effect recorded
// on the source run. There is deliberately no polling or alternate send path.
func (e *AgentExecutor) notifyAwaitingApproval(ctx context.Context, result *ExecuteResult) error {
	if e.approvals == nil || result == nil || result.Status != "awaiting_approval" {
		return nil
	}
	task, err := e.store.LoadTask(ctx, result.TaskID)
	if err != nil {
		return fmt.Errorf("load persisted approval task_id=%d: %w", result.TaskID, err)
	}
	if task.Status != "awaiting_approval" {
		return fmt.Errorf("%w: task_id=%d status=%s changed before approval notification", ErrInvalidTransition, task.ID, task.Status)
	}
	proposal, err := decodeStoredProposal(task.ExecutionResult)
	if err != nil {
		return fmt.Errorf("decode persisted approval task_id=%d: %w", task.ID, err)
	}
	delivery, err := e.approvals.SendApproval(ctx, ApprovalNotification{
		TaskID: task.ID, RunID: result.RunID, Version: task.Version,
		Title: task.Title, Summary: result.Summary,
		Action: proposal.Action, Target: proposal.Target, Artifact: proposal.Artifact,
	})
	if err != nil {
		return err
	}
	if delivery == nil || strings.TrimSpace(delivery.MessageID) == "" {
		return fmt.Errorf("approval notifier returned no message_id for task_id=%d", task.ID)
	}
	run, err := e.store.LoadRun(ctx, result.RunID)
	if err != nil {
		return fmt.Errorf("load approval source run id=%d: %w", result.RunID, err)
	}
	effects, err := appendApprovalCardEffect(run.Effects, delivery)
	if err != nil {
		return fmt.Errorf("record approval card run_id=%d: %w", run.ID, err)
	}
	run.Effects = datatypes.JSON(effects)
	if err := e.store.SaveRun(ctx, run); err != nil {
		return fmt.Errorf("save approval card effect run_id=%d: %w", run.ID, err)
	}
	return nil
}

func appendApprovalCardEffect(raw []byte, delivery *ApprovalDelivery) ([]byte, error) {
	var effects []map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &effects); err != nil {
			return nil, fmt.Errorf("decode existing effects: %w", err)
		}
	}
	extra, err := json.Marshal(map[string]any{"message_id": delivery.MessageID, "purpose": "approval"})
	if err != nil {
		return nil, fmt.Errorf("encode approval effect extra: %w", err)
	}
	effects = append(effects, map[string]any{
		"kind": "feishu_message", "title": "审批卡片", "url": delivery.URL,
		"target": delivery.Target, "preview": delivery.Preview, "extra": string(extra),
		"message_id": delivery.MessageID,
	})
	return json.Marshal(effects)
}

// recordRunProgress stores where the matter now stands, as this run described
// it. A run that parked in waiting or failed outright still moved the matter
// forward, so this is recorded for every terminal state, not just success.
//
// Like observations, this is bookkeeping alongside the real result: the
// execution already happened and its outcome must still be recorded, so a
// failure to store the summary is logged rather than propagated.
func (e *AgentExecutor) recordRunProgress(ctx context.Context, task *domain.Task, run *domain.ExecutionRun) {
	if task == nil || run == nil || len(run.Output) == 0 {
		return
	}
	var structured struct {
		ProgressSummary string `json:"progress_summary"`
	}
	if err := json.Unmarshal(run.Output, &structured); err != nil {
		// The run's own result is already stored; a shape we cannot read here
		// must not turn a finished execution into a failure.
		return
	}
	if err := e.store.RecordProgress(ctx, task.ID, structured.ProgressSummary, e.now()); err != nil {
		hlog.CtxErrorf(ctx, "m5 progress save failed task_id=%d run_id=%d error=%+v", task.ID, run.ID, err)
	}
}

// finishRun persists the terminal state of a run (done on success, failed on
// error), overwriting execution_result with the final verdict, and returns the
// summary result. It is the shared tail for code changes, pure read-only work,
// and the apply stage.
func (e *AgentExecutor) finishRun(ctx context.Context, task *domain.Task, execVersion int32, run *domain.ExecutionRun, execErr error) (*ExecuteResult, error) {
	ctx = context.WithoutCancel(ctx)
	e.recordRunProgress(ctx, task, run)
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
	} else if run.Status == "observing" {
		finishStatus = "observing"
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
		Summary: derefString(run.Summary),
	}
	if execErr != nil {
		return result, fmt.Errorf("execute Task id=%d: %w", task.ID, execErr)
	}
	return result, nil
}

// runOnce builds the prompt and runs codex. Every action_type takes this path:
// the agent investigates, judges approval, and either finishes the work or
// returns a proposal. Any real side effects show up in effects, not dedicated
// action-type columns. Always returns a populated *domain.ExecutionRun; execErr
// is non-nil on any failure so the caller can mark the Task failed while still
// recording what was attempted.
func (e *AgentExecutor) runOnce(ctx context.Context, task *domain.Task) (*domain.ExecutionRun, *codexResult, error) {
	startedAt := e.now().UTC()
	run := &domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Stage: "execute", Sandbox: executionSandbox,
		Status: "running", StartedAt: startedAt,
	}
	if err := e.markRunStarted(ctx, run); err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}

	repoPath, err := e.resolveRepo(task)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	if repoPath != "" {
		run.RepoPath = &repoPath
	}

	previousRuns, err := e.loadPriorRunSummaries(ctx, task.ID, run.ID)
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
		cause := fmt.Errorf("load M5 execution system prompt: %w", err)
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
	prompt, err := buildExecutionPrompt(systemPrompt, approvalPolicy, task, repoPath, toolCatalog, sharedMemory, workRules, skills, previousRuns)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	run.Prompt = prompt

	outputCapture, err := e.prepareTaskRunOutput(task.ID, run.Stage, startedAt, prompt)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	codexOut, err := e.runner.RunTaskWithOutput(ctx, prompt, executionSandbox, repoPath, schemaExecution, task.ID, outputCapture)
	if codexOut != nil {
		recordAgentUsage(run, codexOut.Usage)
	}
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	run.CodexSessionID = &codexOut.SessionID
	if codexOut.Result == nil {
		cause := fmt.Errorf("codex exec returned no structured result")
		run.Summary = &codexOut.LastMessage
		return e.failRun(run, startedAt, cause), nil, cause
	}
	result := codexOut.Result
	if err := recordAgentVerdict(run, result.Summary, result, result.Effects); err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	if result.Outcome == "waiting" {
		e.finishWaitingRun(run, startedAt)
		return run, result, nil
	}
	if result.NeedsApproval {
		e.finishSuccessfulRun(run, startedAt)
		return run, result, nil
	}
	if result.Outcome == "needs_human" {
		e.finishNeedsHumanRun(run, startedAt)
		return run, result, nil
	}
	if result.Outcome == "observing" {
		e.finishObservingRun(run, startedAt)
		return run, result, nil
	}
	if result.Outcome != "completed" {
		cause := fmt.Errorf("task not completed: %s", result.FailureReason)
		return e.failRun(run, startedAt, cause), nil, cause
	}
	e.finishSuccessfulRun(run, startedAt)
	return run, result, nil
}

// runApply lands an approved proposal. It builds a fresh codex invocation with
// the approved proposal + artifact embedded (buildApplyPrompt) and runs it under the
// normal executionResultSchema so the real external write reports a real success
// verdict.
func (e *AgentExecutor) runApply(ctx context.Context, task *domain.Task, proposal *codexProposal) (*domain.ExecutionRun, *codexResult, error) {
	startedAt := e.now().UTC()
	run := &domain.ExecutionRun{
		TaskID: task.ID, ActionType: task.ActionType, Stage: "apply", Sandbox: executionSandbox,
		Status: "running", StartedAt: startedAt,
	}
	if err := e.markRunStarted(ctx, run); err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}

	repoPath, err := e.resolveRepo(task)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	if repoPath != "" {
		run.RepoPath = &repoPath
	}
	previousRuns, err := e.loadPriorRunSummaries(ctx, task.ID, run.ID)
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
	systemPrompt, err := e.textStore.Content(ctx, textstore.SystemPromptM5Key)
	if err != nil {
		cause := fmt.Errorf("load M5 apply system prompt: %w", err)
		return e.failRun(run, startedAt, cause), nil, cause
	}
	approvalPolicy, err := e.textStore.Content(ctx, textstore.ApprovalPolicyKey)
	if err != nil {
		cause := fmt.Errorf("load M5 approval policy for apply: %w", err)
		return e.failRun(run, startedAt, cause), nil, cause
	}
	toolCatalog, err := toolcatalog.Block(toolcatalog.StageExecute)
	if err != nil {
		cause := fmt.Errorf("load M5 tool catalog: %w", err)
		return e.failRun(run, startedAt, cause), nil, cause
	}
	skills, err := e.skills.Catalog(ctx, skill.StageExecute)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	prompt, err := buildApplyPrompt(systemPrompt, approvalPolicy, task, proposal, repoPath, toolCatalog, sharedMemory, workRules, skills, previousRuns)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	run.Prompt = prompt

	outputCapture, err := e.prepareTaskRunOutput(task.ID, run.Stage, startedAt, prompt)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	codexOut, err := e.runner.RunTaskWithOutput(ctx, prompt, executionSandbox, repoPath, schemaExecution, task.ID, outputCapture)
	if codexOut != nil {
		recordAgentUsage(run, codexOut.Usage)
	}
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	run.CodexSessionID = &codexOut.SessionID
	if codexOut.Result == nil {
		cause := fmt.Errorf("codex exec returned no structured result")
		run.Summary = &codexOut.LastMessage
		return e.failRun(run, startedAt, cause), nil, cause
	}
	result := codexOut.Result
	if err := recordAgentVerdict(run, result.Summary, result, result.Effects); err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	if result.Outcome == "waiting" {
		e.finishWaitingRun(run, startedAt)
		return run, result, nil
	}
	// Landing an approved proposal can surface a further gated side effect. The
	// run itself succeeded in that case; routeRun parks the Task for the new ask.
	if result.NeedsApproval {
		e.finishSuccessfulRun(run, startedAt)
		return run, result, nil
	}
	if result.Outcome == "needs_human" {
		e.finishNeedsHumanRun(run, startedAt)
		return run, result, nil
	}
	if result.Outcome != "completed" {
		cause := fmt.Errorf("task not completed: %s", result.FailureReason)
		return e.failRun(run, startedAt, cause), nil, cause
	}
	e.finishSuccessfulRun(run, startedAt)
	return run, result, nil
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

func recordAgentUsage(run *domain.ExecutionRun, usage agentusage.Usage) {
	if run == nil || !usage.Reported {
		return
	}
	input := usage.InputTokens
	cachedInput := usage.CachedInputTokens
	output := usage.OutputTokens
	reasoningOutput := usage.ReasoningOutputTokens
	run.InputTokens = &input
	run.CachedInputTokens = &cachedInput
	run.OutputTokens = &output
	run.ReasoningOutputTokens = &reasoningOutput
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
	return e.store.SaveRun(ctx, run)
}

// markRunStarted lands the run row before the agent is invoked. Until it
// exists, a crash mid-invocation is indistinguishable from a Task that never
// started, and the stale sweep cannot tell which zombies are safe to re-queue.
func (e *AgentExecutor) markRunStarted(ctx context.Context, run *domain.ExecutionRun) error {
	if err := e.persistRun(ctx, run); err != nil {
		return fmt.Errorf("persist started run task_id=%d: %w", run.TaskID, err)
	}
	return nil
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

// resolveRepo validates an explicitly selected Task working copy. Project repo
// metadata never chooses one implicitly; an absent path preserves the server cwd.
func (e *AgentExecutor) resolveRepo(task *domain.Task) (string, error) {
	if task.RepoPath == nil || strings.TrimSpace(*task.RepoPath) == "" {
		return "", nil
	}
	path := e.absRepoPath(*task.RepoPath)
	if isGitDir(path) {
		return path, nil
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

// runEffects returns the run's external-effect list for display: the agent's
// declared effects stored verbatim. Each effect is a loose map so unknown kinds
// and extra fields survive untouched.
func runEffects(run *domain.ExecutionRun) []map[string]any {
	if len(run.Effects) == 0 {
		return nil
	}
	var effects []map[string]any
	if err := json.Unmarshal(run.Effects, &effects); err != nil {
		return nil
	}
	return effects
}

// recordAgentVerdict stores one agent verdict on the run: the human-readable
// summary, the full structured verdict as Output, and the agent's declared
// external effects. The three always travel together — a resumed run that stored
// Output but silently dropped Effects is how Task #82's repeated Feishu pings
// ended up with no recorded side effect to dedupe against.
func recordAgentVerdict(run *domain.ExecutionRun, summary string, verdict any, effects []codexEffect) error {
	if run == nil || verdict == nil {
		return fmt.Errorf("record agent verdict: run and verdict are required")
	}
	run.Summary = &summary
	structured, err := json.Marshal(verdict)
	if err != nil {
		return fmt.Errorf("encode agent verdict: %w", err)
	}
	run.Output = structured
	assignDeclaredEffects(run, effects)
	return nil
}

// assignDeclaredEffects stores the agent's self-declared side effects on the run
// verbatim. Effects are display-only and trusted as reported; they are marshaled
// straight to JSON with no verification. A marshal error leaves Effects unset
// rather than failing the run — losing the display payload must never turn a
// real, completed side effect into a failed Task.
func assignDeclaredEffects(run *domain.ExecutionRun, effects []codexEffect) {
	if len(effects) == 0 {
		return
	}
	encoded, err := json.Marshal(effects)
	if err != nil {
		return
	}
	run.Effects = encoded
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
	if effects := runEffects(run); len(effects) > 0 {
		payload["effects"] = effects
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
func proposalPayload(run *domain.ExecutionRun, result *codexResult) map[string]any {
	payload := map[string]any{
		"stage":         "proposal",
		"action_type":   run.ActionType,
		"source_run_id": run.ID,
		"summary":       result.Summary,
		"proposal": map[string]any{
			"action":   result.Proposal.Action,
			"target":   result.Proposal.Target,
			"artifact": result.Proposal.Artifact,
		},
	}
	if strings.TrimSpace(result.NeedsFollowup) != "" {
		payload["needs_followup"] = result.NeedsFollowup
	}
	if len(result.Enrichments) > 0 {
		payload["enrichments"] = result.Enrichments
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
