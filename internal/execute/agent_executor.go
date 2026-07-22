package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/workrule"

	"gorm.io/gorm"
)

var (
	ErrUnknownActionType = errors.New("unknown action_type has no execution policy")
)

// ExecuteInput drives one Task execution (the propose stage).
type ExecuteInput struct {
	TaskID uint64
}

// ExecuteResult summarizes what happened, for the API/log. When Status is
// awaiting_approval the external write was judged high-risk: codex produced a
// proposal (no outside-world effect yet) and the Task is parked for a human to
// approve or reject.
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
	skills    skill.Reader
	repoRoot  string
	runsDir   string
	now       func() time.Time
}

func NewAgentExecutor(db *gorm.DB, store *Store, runner *CodexRunner, sharedMem sharedmem.SharedMemoryReader, workRules workrule.Reader, skills skill.Reader, repoRoot, runsDir string) (*AgentExecutor, error) {
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
		db: db, store: store, runner: runner, sharedMem: sharedMem, workRules: workRules, skills: skills,
		repoRoot: repoRoot, runsDir: runsDir,
		now: time.Now,
	}, nil
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
	if _, ok := lookupPolicy(task.ActionType); !ok {
		return nil, fmt.Errorf("%w: task_id=%d action_type=%s", ErrUnknownActionType, task.ID, task.ActionType)
	}
	execVersion, err := e.store.MarkExecuting(ctx, task.ID, task.Version)
	if err != nil {
		return nil, err
	}
	e.executeClaimedInBackground(task.ID, execVersion)
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
	execVersion, err := e.store.MarkExecuting(ctx, task.ID, task.Version)
	if err != nil {
		return nil, err
	}
	e.executeClaimedInBackground(task.ID, execVersion)
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
	execVersion, err := e.store.ClaimForReapply(ctx, task.ID, task.Version)
	if err != nil {
		return nil, err
	}
	claimed := task
	go func() {
		if _, err := e.applyApproved(context.Background(), &claimed, policy, proposal, execVersion); err != nil {
			log.Printf("background re-apply task_id=%d: %v", claimed.ID, err)
		}
	}()
	return &ExecuteResult{TaskID: task.ID, Status: "executing"}, nil
}

func (e *AgentExecutor) executeClaimedInBackground(taskID uint64, execVersion int32) {
	go func() {
		if _, err := e.executeClaimed(context.Background(), taskID, execVersion); err != nil {
			log.Printf("background execute task_id=%d: %v", taskID, err)
		}
	}()
}

// KickApprove lands an accepted proposal in the background. It synchronously
// validates and claims the awaiting_approval Task (-> executing, under optimistic
// lock) so version/state conflicts surface immediately to the caller, then runs
// the apply stage (a fresh codex invocation) in a goroutine and returns at once.
// The apply codex call can be slow; the HTTP handler must not block on it. Poll
// Task status or refresh the list for completion.
func (e *AgentExecutor) KickApprove(ctx context.Context, taskID uint64, expectedVersion int32) (*ExecuteResult, error) {
	task, policy, proposal, execVersion, err := e.claimForApproval(ctx, taskID, expectedVersion)
	if err != nil {
		return nil, err
	}
	go func() {
		if _, err := e.applyApproved(context.Background(), task, policy, proposal, execVersion); err != nil {
			log.Printf("background approve task_id=%d: %v", task.ID, err)
		}
	}()
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

	// Claim the Task (pending -> executing). This is the concurrency guard.
	execVersion, err := e.store.MarkExecuting(ctx, task.ID, task.Version)
	if err != nil {
		return nil, err
	}
	return e.executeClaimed(ctx, task.ID, execVersion)
}

// executeClaimed runs an already-claimed (executing) Task. Used by KickExecute /
// KickRerun after a synchronous claim, and by Execute after MarkExecuting.
func (e *AgentExecutor) executeClaimed(ctx context.Context, taskID uint64, execVersion int32) (*ExecuteResult, error) {
	var task domain.Task
	if err := e.db.WithContext(ctx).First(&task, taskID).Error; err != nil {
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
	// without a human. Every OTHER action_type runs the propose stage first, where
	// the agent judges — by its actual intended behavior, not a static label —
	// whether it will write/send/modify the outside world. If so it stops and
	// produces a proposal for human approval; if it is only reading/querying, it
	// finishes in place. This closes the "an investigate Task decides mid-run to
	// send a message" gap that a pre-assigned external flag would miss.
	if runsToCompletion(task.ActionType) {
		run, execErr := e.runOnce(ctx, &task, policy)
		if writeErr := e.persistRun(ctx, run); writeErr != nil {
			return nil, fmt.Errorf("persist execution run task_id=%d: %w", task.ID, writeErr)
		}
		return e.finishRun(ctx, &task, execVersion, run, execErr)
	}
	return e.executePropose(ctx, &task, policy, execVersion)
}

// runsToCompletion reports whether an action_type skips the propose/approval gate
// and runs straight to completion. Only code_change qualifies: its landing is a
// pushed branch + MR, which a human must still merge, so the MR is the review
// gate. All other action types go through propose so the agent can flag any real
// external write for approval based on what it actually intends to do.
func runsToCompletion(actionType string) bool {
	return actionType == "code_change"
}

// executePropose runs the propose stage (every action except code_change) and
// routes the outcome: if the agent declared it will write to the outside world
// (needs_approval=true) the Task parks at awaiting_approval with the proposal
// stored; otherwise the read-only/local work is finished in place (done/failed).
func (e *AgentExecutor) executePropose(ctx context.Context, task *domain.Task, policy actionPolicy, execVersion int32) (*ExecuteResult, error) {
	run, propose, execErr := e.runPropose(ctx, task, policy)
	if writeErr := e.persistRun(ctx, run); writeErr != nil {
		return nil, fmt.Errorf("persist execution run task_id=%d: %w", task.ID, writeErr)
	}
	if execErr != nil {
		return e.finishRun(ctx, task, execVersion, run, execErr)
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
	// Low-risk: codex already did the work; finish done/failed on its verdict.
	if !propose.Success {
		cause := fmt.Errorf("task not completed: %s", propose.FailureReason)
		return e.finishRun(ctx, task, execVersion, run, cause)
	}
	return e.finishRun(ctx, task, execVersion, run, nil)
}

// finishRun persists the terminal state of a run (done on success, failed on
// error), overwriting execution_result with the final verdict, and returns the
// summary result. It is the shared tail for local actions, low-risk external
// actions, and the apply stage.
func (e *AgentExecutor) finishRun(ctx context.Context, task *domain.Task, execVersion int32, run *domain.ExecutionRun, execErr error) (*ExecuteResult, error) {
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
		ActorType: "m5", RunID: &run.ID,
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
		TaskID: task.ID, ActionType: task.ActionType, Sandbox: policy.sandbox,
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
	prompt, err := buildExecutionPrompt(task, repoPath, sharedMemory, workRules, skills, previousRuns)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	run.Prompt = prompt

	codexOut, err := e.runner.Run(ctx, prompt, policy.sandbox, repoPath, schemaExecution)
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
	if !codexOut.Result.Success {
		cause := fmt.Errorf("task not completed: %s", codexOut.Result.FailureReason)
		return e.failRun(run, startedAt, cause), cause
	}

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
			// Full-autonomy: push the branch and open an MR. A push/MR failure is a
			// real failure of a code_change Task (the change never leaves the box),
			// so fail-fast rather than silently leaving it local.
			pushOut, err := repo.pushBranchWithMR(ctx, *run.Branch, baseBranch)
			if err != nil {
				return e.failRun(run, startedAt, err), err
			}
			if url := extractMergeRequestURL(pushOut); url != "" {
				run.MergeRequestURL = &url
			}
		}
	}

	finished := e.now().UTC()
	run.Status = "succeeded"
	run.FinishedAt = &finished
	ms := finished.Sub(startedAt).Milliseconds()
	run.DurationMs = &ms
	return run, nil
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
		TaskID: task.ID, ActionType: task.ActionType, Sandbox: policy.sandbox,
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
	prompt, err := buildProposePrompt(task, sharedMemory, workRules, skills, previousRuns)
	if err != nil {
		return e.failRun(run, startedAt, err), nil, err
	}
	run.Prompt = prompt

	codexOut, err := e.runner.Run(ctx, prompt, policy.sandbox, "", schemaPropose)
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
		TaskID: task.ID, ActionType: task.ActionType, Sandbox: policy.sandbox,
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
	skills, err := e.skills.Catalog(ctx, skill.StageExecute)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	prompt, err := buildApplyPrompt(task, proposal, sharedMemory, workRules, skills, previousRuns)
	if err != nil {
		return e.failRun(run, startedAt, err), err
	}
	run.Prompt = prompt

	codexOut, err := e.runner.Run(ctx, prompt, policy.sandbox, "", schemaExecution)
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
	if !codexOut.Result.Success {
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

func (e *AgentExecutor) persistRun(ctx context.Context, run *domain.ExecutionRun) error {
	return e.db.WithContext(ctx).Create(run).Error
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
	payload := map[string]any{
		"stage":       "executed",
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
			if strings.TrimSpace(structured.NeedsFollowup) != "" {
				payload["needs_followup"] = structured.NeedsFollowup
			}
			if len(structured.Enrichments) > 0 {
				payload["enrichments"] = structured.Enrichments
			}
		}
	}
	if execErr != nil {
		payload["error"] = execErr.Error()
	}
	return payload
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
