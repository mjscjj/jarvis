package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const maxCodexOutputBytes = 1 << 20

// codexRun is the raw outcome of one codex CLI invocation. At most one of
// Result / Propose is populated, chosen by the schema the caller enforced;
// both are nil for RunText callers that enforce no schema.
type codexRun struct {
	SessionID   string
	LastMessage string
	// Result is the structured verdict codex returns per executionResultSchema
	// (the apply / low-risk path). It is nil unless that schema was enforced.
	Result *codexResult
	// Propose is the risk-judgement verdict codex returns per proposeResultSchema
	// (the propose stage of an external-side-effect action). It is nil unless
	// that schema was enforced.
	Propose *proposeResult
}

type runInvocation struct {
	SessionID string
	TaskID    uint64
	Output    *codexOutputCapture
}

type codexOutputCapture struct {
	StdoutPath string
	StderrPath string
}

// codexResult is the structured final message codex must return for M5
// execution (see executionResultSchema). It lets M5 判 done/failed on a real
// success bool instead of the process exit code.
type codexResult struct {
	Outcome       string            `json:"outcome"`
	Summary       string            `json:"summary"`
	FailureReason string            `json:"failure_reason"`
	NeedsFollowup string            `json:"needs_followup"`
	Enrichments   []codexEnrichment `json:"enrichments"`
	Waiting       *codexWaiting     `json:"waiting"`
}

type codexWaiting struct {
	ScheduledTaskID uint64 `json:"scheduled_task_id"`
	WakeAt          string `json:"wake_at"`
	Reason          string `json:"reason"`
}

// codexEnrichment is one piece of "多做一步" context the assistant proactively
// prepared (a code link, a commit digest, a doc link, …).
type codexEnrichment struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

// proposeResult is the structured final message codex must return for the
// propose stage of a non-code action (see proposeResultSchema). When
// NeedsApproval is false the agent already finished pure read-only work. When it
// is true the agent performed no mutation; Proposal holds the plan + full
// artifact awaiting human approval before apply lands it.
type proposeResult struct {
	NeedsApproval bool              `json:"needs_approval"`
	Outcome       string            `json:"outcome"`
	Summary       string            `json:"summary"`
	FailureReason string            `json:"failure_reason"`
	NeedsFollowup string            `json:"needs_followup"`
	Enrichments   []codexEnrichment `json:"enrichments"`
	Proposal      *codexProposal    `json:"proposal"`
	Waiting       *codexWaiting     `json:"waiting"`
}

// codexProposal is the concrete mutation the agent wants a human to approve:
// what it will do, which object it targets, and the complete artifact (file
// content, changed document, exact message, meeting request, …).
type codexProposal struct {
	Action   string `json:"action"`
	Target   string `json:"target"`
	Artifact string `json:"artifact"`
}

// CodexRunner wraps the codex CLI for execution. On this trusted local host runs
// use danger-full-access so external tools (lark-cli/bytedcli) can reach the
// network and macOS Keychain; the safety boundary is the propose/approval gate
// (the agent declares needs_approval before any local or external mutation), not
// the sandbox.
type CodexRunner struct {
	bin             string
	model           string
	reasoningEffort string
	timeout         time.Duration
}

func NewCodexRunner(bin, model, reasoningEffort string, timeout time.Duration) (*CodexRunner, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, fmt.Errorf("codex runner bin is required")
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("find codex runner binary %q: %w", bin, err)
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("codex runner model is required")
	}
	switch strings.TrimSpace(reasoningEffort) {
	case "minimal", "low", "medium", "high", "xhigh":
	default:
		return nil, fmt.Errorf("codex runner reasoning_effort must be minimal/low/medium/high/xhigh, got %q", reasoningEffort)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("codex runner timeout must be positive")
	}
	return &CodexRunner{bin: resolved, model: model, reasoningEffort: reasoningEffort, timeout: timeout}, nil
}

// Schema selects which structured final-message contract Run enforces:
//   - schemaNone: no schema; codexRun.Result and .Propose stay nil (RunText).
//   - schemaExecution: executionResultSchema; parsed into codexRun.Result (the
//     apply / low-risk landing verdict).
//   - schemaPropose: proposeResultSchema; parsed into codexRun.Propose (the
//     mutation judgement + proposal).
type schema int

const (
	schemaNone schema = iota
	schemaExecution
	schemaPropose
)

func (s schema) definition() (string, bool) {
	switch s {
	case schemaExecution:
		return executionResultSchema, true
	case schemaPropose:
		return proposeResultSchema, true
	default:
		return "", false
	}
}

// Run executes codex once with the given prompt and sandbox. sandbox must be
// "read-only", "workspace-write" or "danger-full-access". repoPath, when
// non-empty, is the working directory codex operates in. The schema argument
// selects which structured final-message contract the run enforces and parses.
func (r *CodexRunner) Run(ctx context.Context, prompt, sandbox, repoPath string, sch schema) (*codexRun, error) {
	return r.run(ctx, prompt, sandbox, repoPath, sch, runInvocation{})
}

// RunTask starts a persisted Codex session for one Task. The Task ID is exposed
// to controlled Jarvis tools so the agent can park itself with yield-until.
func (r *CodexRunner) RunTask(ctx context.Context, prompt, sandbox, repoPath string, sch schema, taskID uint64) (*codexRun, error) {
	return r.RunTaskWithOutput(ctx, prompt, sandbox, repoPath, sch, taskID, nil)
}

func (r *CodexRunner) RunTaskWithOutput(ctx context.Context, prompt, sandbox, repoPath string, sch schema, taskID uint64, output *codexOutputCapture) (*codexRun, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("codex task run requires a positive task ID")
	}
	return r.run(ctx, prompt, sandbox, repoPath, sch, runInvocation{TaskID: taskID, Output: output})
}

// ResumeTask starts another turn in an existing persisted Codex session.
func (r *CodexRunner) ResumeTask(ctx context.Context, sessionID, prompt, sandbox, repoPath string, sch schema, taskID uint64) (*codexRun, error) {
	return r.ResumeTaskWithOutput(ctx, sessionID, prompt, sandbox, repoPath, sch, taskID, nil)
}

func (r *CodexRunner) ResumeTaskWithOutput(ctx context.Context, sessionID, prompt, sandbox, repoPath string, sch schema, taskID uint64, output *codexOutputCapture) (*codexRun, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("codex resume session ID is required")
	}
	if taskID == 0 {
		return nil, fmt.Errorf("codex resume requires a positive task ID")
	}
	return r.run(ctx, prompt, sandbox, repoPath, sch, runInvocation{SessionID: sessionID, TaskID: taskID, Output: output})
}

func (r *CodexRunner) run(ctx context.Context, prompt, sandbox, repoPath string, sch schema, invocation runInvocation) (*codexRun, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("codex run prompt is required")
	}
	switch sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("codex run sandbox must be read-only, workspace-write or danger-full-access, got %q", sandbox)
	}
	if sandbox == "workspace-write" && strings.TrimSpace(repoPath) == "" {
		return nil, fmt.Errorf("codex workspace-write run requires a repo path")
	}

	tempDir, err := os.MkdirTemp("", "jarvis-codex-exec-")
	if err != nil {
		return nil, fmt.Errorf("create codex exec temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	resultPath := filepath.Join(tempDir, "last-message.txt")

	// codex exec is non-interactive; --sandbox is the sole gate (no
	// --ask-for-approval, which only exists in interactive mode). We never pass
	// --dangerously-bypass-approvals-and-sandbox: the sandbox stays enforced.
	var args []string
	if invocation.SessionID == "" {
		args = []string{
			"exec", "--sandbox", sandbox,
			"--color", "never", "--json", "--output-last-message", resultPath,
			"--model", r.model, "-c", "model_reasoning_effort=" + r.reasoningEffort,
		}
		if invocation.TaskID == 0 {
			// Only M5 Task runs need a durable session for yield/resume. Free-form
			// summarizers and insight generators are one-shot calls and must not
			// accumulate resumable Codex sessions on disk.
			args = append(args, "--ephemeral")
		}
	} else {
		args = []string{
			"exec", "resume", invocation.SessionID, "--json",
			"--output-last-message", resultPath,
			"--model", r.model,
			"-c", "model_reasoning_effort=" + r.reasoningEffort,
			"-c", fmt.Sprintf("sandbox_mode=%q", sandbox),
		}
	}
	schemaDef, enforceSchema := sch.definition()
	if enforceSchema {
		schemaPath := filepath.Join(tempDir, "result-schema.json")
		if err := os.WriteFile(schemaPath, []byte(schemaDef), 0o600); err != nil {
			return nil, fmt.Errorf("write codex exec result schema: %w", err)
		}
		args = append(args, "--output-schema", schemaPath)
	}
	if sandbox == "workspace-write" {
		// workspace-write disables network by default; re-enable it so codex can
		// call lark-cli/bytedcli. danger-full-access already has network.
		args = append(args, "-c", "sandbox_workspace_write.network_access=true")
	}
	if strings.TrimSpace(repoPath) != "" && invocation.SessionID == "" {
		args = append(args, "--cd", repoPath)
	} else {
		args = append(args, "--skip-git-repo-check")
	}
	args = append(args, "-")

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, r.bin, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if strings.TrimSpace(repoPath) == "" && invocation.TaskID == 0 {
		command.Dir = tempDir
	} else if strings.TrimSpace(repoPath) != "" {
		command.Dir = repoPath
	}
	if invocation.TaskID != 0 {
		command.Env = append(os.Environ(), fmt.Sprintf("JARVIS_TASK_ID=%d", invocation.TaskID))
	}
	command.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	stdoutWriter := io.Writer(&stdout)
	stderrWriter := io.Writer(&stderr)
	var stdoutFile, stderrFile *os.File
	if invocation.Output != nil {
		stdoutFile, err = os.OpenFile(invocation.Output.StdoutPath, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return nil, fmt.Errorf("open codex stdout capture %q: %w", invocation.Output.StdoutPath, err)
		}
		defer stdoutFile.Close()
		stderrFile, err = os.OpenFile(invocation.Output.StderrPath, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return nil, fmt.Errorf("open codex stderr capture %q: %w", invocation.Output.StderrPath, err)
		}
		defer stderrFile.Close()
		stdoutWriter = io.MultiWriter(&stdout, stdoutFile)
		stderrWriter = io.MultiWriter(&stderr, stderrFile)
	}
	command.Stdout = stdoutWriter
	command.Stderr = stderrWriter
	if err := command.Run(); err != nil {
		if errors.Is(context.Cause(runCtx), ErrExecutionInterrupted) {
			return nil, ErrExecutionInterrupted
		}
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("codex exec timed out after %s", r.timeout)
		}
		return nil, fmt.Errorf("codex exec failed: %w: %s", err, limitedText(stderr.Bytes(), 4096))
	}

	sessionID, err := codexSessionID(stdout.Bytes())
	if err != nil {
		return nil, err
	}
	last, err := readLimitedFile(resultPath, maxCodexOutputBytes)
	if err != nil {
		return nil, err
	}
	lastMessage := string(bytes.TrimSpace(last))
	run := &codexRun{SessionID: sessionID, LastMessage: lastMessage}
	switch sch {
	case schemaExecution:
		result, err := parseExecutionResult(lastMessage)
		if err != nil {
			return nil, err
		}
		run.Result = result
	case schemaPropose:
		propose, err := parseProposeResult(lastMessage)
		if err != nil {
			return nil, err
		}
		run.Propose = propose
	}
	return run, nil
}

// parseExecutionResult decodes codex's schema-constrained final message. It is
// strict (fail-fast): a malformed or empty result is an execution failure, not
// a silent success.
func parseExecutionResult(lastMessage string) (*codexResult, error) {
	trimmed := strings.TrimSpace(lastMessage)
	if trimmed == "" {
		return nil, fmt.Errorf("codex exec returned empty result message")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var result codexResult
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode codex exec result %q: %w", limitedText([]byte(trimmed), 512), err)
	}
	if strings.TrimSpace(result.Summary) == "" {
		return nil, fmt.Errorf("codex exec result summary is blank")
	}
	if err := validateOutcome(result.Outcome, result.FailureReason, result.NeedsFollowup, result.Waiting); err != nil {
		return nil, fmt.Errorf("codex exec result: %w", err)
	}
	return &result, nil
}

// parseProposeResult decodes codex's propose-stage final message. It is strict
// (fail-fast): needs_approval=true MUST carry a non-empty proposal (action +
// target + artifact) — a "please approve" verdict with no artifact is useless
// and is treated as an execution failure, not a silent stop.
func parseProposeResult(lastMessage string) (*proposeResult, error) {
	trimmed := strings.TrimSpace(lastMessage)
	if trimmed == "" {
		return nil, fmt.Errorf("codex propose returned empty result message")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var result proposeResult
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode codex propose result %q: %w", limitedText([]byte(trimmed), 512), err)
	}
	if strings.TrimSpace(result.Summary) == "" {
		return nil, fmt.Errorf("codex propose result summary is blank")
	}
	if result.NeedsApproval {
		if result.Outcome != "needs_human" {
			return nil, fmt.Errorf("codex propose needs_approval=true requires outcome=needs_human")
		}
		if result.Proposal == nil {
			return nil, fmt.Errorf("codex propose result needs_approval=true requires a proposal")
		}
		if strings.TrimSpace(result.Proposal.Action) == "" ||
			strings.TrimSpace(result.Proposal.Target) == "" ||
			strings.TrimSpace(result.Proposal.Artifact) == "" {
			return nil, fmt.Errorf("codex propose result proposal must have non-empty action, target and artifact")
		}
	} else if err := validateOutcome(result.Outcome, result.FailureReason, result.NeedsFollowup, result.Waiting); err != nil {
		return nil, fmt.Errorf("codex propose result: %w", err)
	}
	return &result, nil
}

func validateOutcome(outcome, failureReason, needsFollowup string, waiting *codexWaiting) error {
	switch strings.TrimSpace(outcome) {
	case "completed":
		if waiting != nil {
			return fmt.Errorf("outcome=completed requires waiting=null")
		}
	case "waiting":
		if waiting == nil || waiting.ScheduledTaskID == 0 || strings.TrimSpace(waiting.WakeAt) == "" || strings.TrimSpace(waiting.Reason) == "" {
			return fmt.Errorf("outcome=waiting requires scheduled_task_id, wake_at and reason")
		}
	case "needs_human":
		if strings.TrimSpace(needsFollowup) == "" {
			return fmt.Errorf("outcome=needs_human requires needs_followup")
		}
		if waiting != nil {
			return fmt.Errorf("outcome=needs_human requires waiting=null")
		}
	case "failed":
		if strings.TrimSpace(failureReason) == "" {
			return fmt.Errorf("outcome=failed requires failure_reason")
		}
		if waiting != nil {
			return fmt.Errorf("outcome=failed requires waiting=null")
		}
	default:
		return fmt.Errorf("unknown outcome %q", outcome)
	}
	return nil
}

// RunText runs codex read-only and returns just the final message text. It is
// used by lightweight callers (e.g. the Progress digest summarizer) that only
// need free-form prose, not the M5 execution run bookkeeping.
func (r *CodexRunner) RunText(ctx context.Context, prompt string) (string, error) {
	run, err := r.Run(ctx, prompt, "read-only", "", schemaNone)
	if err != nil {
		return "", err
	}
	return run.LastMessage, nil
}

// RunTextSandbox runs codex with the caller-chosen sandbox and returns just the
// final message text. It exists for callers that must let the agent self-run
// external CLIs (lark-cli/bytedcli/git) — e.g. the daily personal digest, which
// collects "today I did" evidence and therefore needs danger-full-access +
// network, unlike the read-only RunText. Sandbox is validated by Run.
func (r *CodexRunner) RunTextSandbox(ctx context.Context, prompt, sandbox string) (string, error) {
	run, err := r.Run(ctx, prompt, sandbox, "", schemaNone)
	if err != nil {
		return "", err
	}
	return run.LastMessage, nil
}

// codexSessionID extracts the thread_id from codex's JSONL stream. It uses a
// streaming json.Decoder rather than a line scanner because a single JSONL
// event (e.g. a command's captured output) can exceed any fixed line buffer.
// The decoder reads value-by-value and is not bound by line length.
func codexSessionID(output []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	var sessionID string
	for {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("decode codex JSONL stream: %w", err)
		}
		if event.Type == "thread.started" {
			if strings.TrimSpace(event.ThreadID) == "" {
				return "", fmt.Errorf("codex thread.started event is missing thread_id")
			}
			sessionID = event.ThreadID
		}
	}
	if sessionID == "" {
		return "", fmt.Errorf("codex JSONL output is missing thread.started event")
	}
	return sessionID, nil
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open codex exec result: %w", err)
	}
	defer file.Close()
	result, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read codex exec result: %w", err)
	}
	if int64(len(result)) > limit {
		return nil, fmt.Errorf("codex exec result exceeds %d bytes", limit)
	}
	return result, nil
}

func limitedText(b []byte, limit int) string {
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "...(truncated)"
}
