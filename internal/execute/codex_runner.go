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

	"jarvis/internal/agentusage"
)

const maxCodexOutputBytes = 1 << 20

// resumeProbeTimeout bounds the one-off `exec resume --help` capability probe
// run when a CodexRunner is constructed.
const resumeProbeTimeout = 20 * time.Second

// maxResumeSchemaRewrites is how many extra times a session may be asked to
// restate its final message after returning one that broke the contract. It
// applies to fresh runs too: --output-schema describes the contract to the
// model rather than constraining decoding, so any turn can end malformed.
const maxResumeSchemaRewrites = 2

// ErrSchemaViolation marks a final message that did not satisfy the required
// result contract. Resume turns that cannot pass --output-schema use it to tell
// a contract break apart from a process failure and ask for a rewrite.
var ErrSchemaViolation = errors.New("agent final message violates the required schema")

// codexRun is the raw outcome of one codex CLI invocation. Result is nil for
// RunText callers that enforce no schema.
type codexRun struct {
	SessionID   string
	LastMessage string
	Usage       agentusage.Usage
	// Result is the structured verdict codex returns per executionResultSchema,
	// which every task-executing stage shares. It is nil unless that schema was
	// enforced.
	Result *codexResult
}

type runInvocation struct {
	SessionID  string
	TaskID     uint64
	Output     *codexOutputCapture
	AgentStage string
}

type codexOutputCapture struct {
	StdoutPath string
	StderrPath string
}

// codexResult is the structured final message codex must return for M5
// execution (see executionResultSchema). It lets M5 判 done/failed on a real
// success bool instead of the process exit code.
type codexResult struct {
	// NeedsApproval is the agent's own verdict, under the injected approval
	// policy, on the side effect it is about to cause. When true it performed no
	// mutation and Proposal holds the plan + full artifact awaiting review; when
	// false it was free to finish the work in place.
	NeedsApproval bool           `json:"needs_approval"`
	Proposal      *codexProposal `json:"proposal"`
	Outcome       string         `json:"outcome"`
	Summary       string         `json:"summary"`
	// ProgressSummary is where the whole matter stands, spanning every run of the
	// Task, whereas Summary covers only this run. Empty means "this run moved
	// nothing", and the stored Task.Summary is left as it was.
	ProgressSummary string            `json:"progress_summary"`
	FailureReason   string            `json:"failure_reason"`
	NeedsFollowup   string            `json:"needs_followup"`
	Enrichments     []codexEnrichment `json:"enrichments"`
	Effects         []codexEffect     `json:"effects"`
	Waiting         *codexWaiting     `json:"waiting"`
}

type codexWaiting struct {
	ScheduledTaskID uint64 `json:"scheduled_task_id"`
	WakeAt          string `json:"wake_at"`
	Reason          string `json:"reason"`
}

// codexEnrichment is one open semantic block the assistant proactively prepared
// (a code link, a commit digest, a doc link, a risk note, ...). kind/label stay
// structured for lightweight UI routing; content is a free-form string (the
// agent may embed JSON text there if it needs structure). content stays a
// string because codex enforces the schema via OpenAI Structured Outputs, whose
// strict validator rejects property nodes without a concrete "type".
type codexEnrichment struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Content string `json:"content"`
}

// codexEffect is one real-world side effect the agent declares it produced (a
// feishu message it sent, a doc it created, a meeting it scheduled, an MR it
// opened, a permission it requested, ...). It is display-only. Kind is free-form
// (agents may invent new kinds). OpenAI Structured Outputs force
// additionalProperties=false on the schema item, so any metadata beyond the
// known keys must travel in the optional "extra" JSON-text field; the parser
// still accepts leftover top-level keys (hand/legacy payloads) into Extra.
// Jarvis does not verify these against lark-cli/git receipts.
type codexEffect struct {
	Kind    string
	Title   string
	URL     string
	Target  string
	Preview string
	// Extra holds metadata beyond the known keys: either expanded from the
	// schema "extra" JSON-text field, or leftover top-level keys from lenient
	// / legacy payloads. Flattened again on MarshalJSON for the UI.
	Extra map[string]json.RawMessage
}

// UnmarshalJSON decodes one effect leniently: it pulls the known keys (including
// optional "extra" JSON text) and stashes leftover top-level fields in Extra.
// When "extra" is a JSON object string, its keys are merged into Extra for UI
// display; otherwise the raw string is kept under Extra["extra"]. Unknown
// top-level fields never fail parsing — only the Structured Outputs schema
// rejects them at generation time.
func (e *codexEffect) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode effect object: %w", err)
	}
	take := func(key string) (string, bool) {
		v, ok := raw[key]
		if !ok {
			return "", false
		}
		delete(raw, key)
		var str string
		if err := json.Unmarshal(v, &str); err == nil {
			return str, true
		}
		return strings.TrimSpace(string(v)), true
	}
	e.Kind, _ = take("kind")
	e.Title, _ = take("title")
	e.URL, _ = take("url")
	e.Target, _ = take("target")
	e.Preview, _ = take("preview")
	extra, hasExtra := take("extra")
	e.Extra = nil
	if hasExtra && strings.TrimSpace(extra) != "" {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal([]byte(extra), &nested); err == nil {
			e.Extra = nested
		} else {
			encoded, _ := json.Marshal(extra)
			e.Extra = map[string]json.RawMessage{"extra": encoded}
		}
	}
	if len(raw) > 0 {
		if e.Extra == nil {
			e.Extra = raw
		} else {
			for k, v := range raw {
				e.Extra[k] = v
			}
		}
	}
	return nil
}

// MarshalJSON re-emits the effect as one flat object for UI display: known keys
// plus Extra fields expanded at the top level. Extra is preferred over an
// opaque "extra" blob so Task detail can render message_id/doc_token/… rows.
func (e codexEffect) MarshalJSON() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(e.Extra)+5)
	for k, v := range e.Extra {
		out[k] = v
	}
	set := func(key, val string) {
		if strings.TrimSpace(val) == "" {
			return
		}
		encoded, _ := json.Marshal(val)
		out[key] = encoded
	}
	set("kind", e.Kind)
	set("title", e.Title)
	set("url", e.URL)
	set("target", e.Target)
	set("preview", e.Preview)
	return json.Marshal(out)
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
// network and macOS Keychain; the safety boundary is M5's approval pause
// (the agent declares needs_approval before any local or external mutation), not
// the sandbox.
type CodexRunner struct {
	bin             string
	model           string
	reasoningEffort string
	timeout         time.Duration
	// resumeOutputSchema records whether this CLI accepts --output-schema on
	// `exec resume`. Official codex does; traecli only accepts it on a fresh
	// `exec`. When false, resume turns carry the contract in the prompt and are
	// validated locally instead.
	resumeOutputSchema bool
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
	resumeOutputSchema, err := resumeAcceptsOutputSchema(resolved)
	if err != nil {
		return nil, err
	}
	return &CodexRunner{
		bin: resolved, model: model, reasoningEffort: reasoningEffort, timeout: timeout,
		resumeOutputSchema: resumeOutputSchema,
	}, nil
}

// resumeAcceptsOutputSchema asks the CLI itself whether `exec resume` takes
// --output-schema, rather than branching on the binary name. Official codex
// accepts it; traecli rejects it with "unexpected argument", which would abort
// every yield-until continuation and needs_human reply.
func resumeAcceptsOutputSchema(bin string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), resumeProbeTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, bin, "exec", "resume", "--help").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf(
			"probe %q exec resume --help: %w: %s", bin, err, limitedText(output, 2048),
		)
	}
	return bytes.Contains(output, []byte("--output-schema")), nil
}

// Schema selects which structured final-message contract Run enforces:
//   - schemaNone: no schema; codexRun.Result stays nil (RunText).
//   - schemaExecution: executionResultSchema; parsed into codexRun.Result. Every
//     task-executing stage shares it, including the approval verdict.
type schema int

const (
	schemaNone schema = iota
	schemaExecution
)

func (s schema) definition() (string, bool) {
	switch s {
	case schemaExecution:
		return executionResultSchema, true
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
	run, err := r.run(ctx, prompt, sandbox, repoPath, sch, runInvocation{TaskID: taskID, Output: output})
	if err == nil || !errors.Is(err, ErrSchemaViolation) || run == nil {
		return run, err
	}
	// --output-schema only describes the contract to the model, it does not
	// constrain decoding, so a fresh exec can also end on a malformed final
	// message. The Task's real side effects already happened, so ask the same
	// session to restate them; never re-run the Task.
	rewritten, rewriteErr := r.rewriteFinalMessage(ctx, run.SessionID, sandbox, repoPath, sch, taskID, output, err)
	if rewritten == nil {
		return run, rewriteErr
	}
	if addErr := rewritten.Usage.Add(run.Usage); addErr != nil {
		return rewritten, fmt.Errorf("combine codex rewrite usage: %w", addErr)
	}
	return rewritten, rewriteErr
}

// rewriteFinalMessage resumes a session whose work is done but whose final
// message did not parse, and asks only for the report to be written again.
func (r *CodexRunner) rewriteFinalMessage(
	ctx context.Context, sessionID, sandbox, repoPath string, sch schema,
	taskID uint64, output *codexOutputCapture, cause error,
) (*codexRun, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, cause
	}
	invocation := runInvocation{SessionID: sessionID, TaskID: taskID, Output: output}
	var priorUsage agentusage.Usage
	for attempt := 0; ; attempt++ {
		run, err := r.run(ctx, schemaRewritePrompt(cause), sandbox, repoPath, sch, invocation)
		if run != nil {
			if addErr := run.Usage.Add(priorUsage); addErr != nil {
				return run, fmt.Errorf("combine codex rewrite usage: %w", addErr)
			}
		}
		if err == nil {
			return run, nil
		}
		if !errors.Is(err, ErrSchemaViolation) || attempt >= maxResumeSchemaRewrites {
			return run, err
		}
		priorUsage = run.Usage
		cause = err
	}
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
	invocation := runInvocation{SessionID: sessionID, TaskID: taskID, Output: output}
	if _, enforceSchema := sch.definition(); !enforceSchema {
		return r.run(ctx, prompt, sandbox, repoPath, sch, invocation)
	}
	// The CLI can end a turn with a malformed final message whether or not it
	// took --output-schema, since that flag describes the contract rather than
	// constraining decoding. Hand the violation back to the same session for a
	// rewrite instead of failing the Task on the first bad turn; the work itself
	// is already done and re-running it would repeat real side effects.
	var priorUsage agentusage.Usage
	for attempt := 0; ; attempt++ {
		run, err := r.run(ctx, prompt, sandbox, repoPath, sch, invocation)
		if run != nil {
			if addErr := run.Usage.Add(priorUsage); addErr != nil {
				return run, fmt.Errorf("combine codex resume usage: %w", addErr)
			}
		}
		if err == nil {
			return run, nil
		}
		if !errors.Is(err, ErrSchemaViolation) || attempt >= maxResumeSchemaRewrites {
			return run, err
		}
		priorUsage = run.Usage
		prompt = schemaRewritePrompt(err)
	}
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
	// When the CLI cannot enforce the contract with --output-schema (traecli on
	// `exec resume`), the schema travels in the prompt and the parser below is
	// the only gate.
	schemaDef, enforceSchema := sch.definition()
	inlineSchema := enforceSchema && invocation.SessionID != "" && !r.resumeOutputSchema
	if inlineSchema {
		prompt = appendSchemaContract(prompt, schemaDef)
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
	if enforceSchema && !inlineSchema {
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
	agentStage := strings.TrimSpace(invocation.AgentStage)
	if agentStage == "" {
		agentStage = "execute"
	}
	if !validAgentStage(agentStage) {
		return nil, fmt.Errorf("codex run agent stage is invalid: %q", agentStage)
	}
	command.Env = codexEnvironment(os.Environ(), invocation.TaskID, agentStage)
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
	commandErr := command.Run()
	usage, usageErr := agentusage.ParseCodexJSONL(stdout.Bytes())
	partialRun := &codexRun{Usage: usage}
	if commandErr != nil {
		var runErr error
		switch {
		case errors.Is(context.Cause(runCtx), ErrExecutionInterrupted):
			runErr = ErrExecutionInterrupted
		case runCtx.Err() == context.DeadlineExceeded:
			runErr = fmt.Errorf("codex exec timed out after %s", r.timeout)
		default:
			runErr = fmt.Errorf("codex exec failed: %w: %s", commandErr, limitedText(stderr.Bytes(), 4096))
		}
		if usageErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("parse failed codex exec usage: %w", usageErr))
		}
		return partialRun, runErr
	}
	if usageErr != nil {
		return partialRun, fmt.Errorf("parse codex exec usage: %w", usageErr)
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
	run := &codexRun{SessionID: sessionID, LastMessage: lastMessage, Usage: usage}
	switch sch {
	case schemaExecution:
		result, err := parseExecutionResult(lastMessage)
		if err != nil {
			// The run itself happened; only its report is unreadable. Hand the
			// session back so the caller can ask for a rewrite instead of
			// throwing away real work.
			return run, fmt.Errorf("%w: %w", ErrSchemaViolation, err)
		}
		run.Result = result
	}
	return run, nil
}

// codexEnvironment removes Jarvis invocation metadata inherited from the
// parent process before setting the metadata for this invocation. In
// particular, one-shot agents must never impersonate the Task that happened to
// launch them.
func codexEnvironment(base []string, taskID uint64, agentStage string) []string {
	environment := make([]string, 0, len(base)+2)
	for _, entry := range base {
		if strings.HasPrefix(entry, "JARVIS_TASK_ID=") || strings.HasPrefix(entry, "JARVIS_AGENT_STAGE=") {
			continue
		}
		environment = append(environment, entry)
	}
	environment = append(environment, "JARVIS_AGENT_STAGE="+agentStage)
	if taskID != 0 {
		environment = append(environment, fmt.Sprintf("JARVIS_TASK_ID=%d", taskID))
	}
	return environment
}

// parseExecutionResult decodes codex's schema-constrained final message, for
// every stage. It is strict (fail-fast): a malformed or empty result is an
// execution failure, not a silent success, and needs_approval=true MUST carry a
// non-empty proposal (action + target + artifact) — a "please approve" verdict
// with no artifact is useless and is treated as a failure, not a silent stop.
func parseExecutionResult(lastMessage string) (*codexResult, error) {
	trimmed := strings.TrimSpace(lastMessage)
	if trimmed == "" {
		return nil, fmt.Errorf("codex exec returned empty result message")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	var result codexResult
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode codex exec result %q: %w", limitedText([]byte(trimmed), 512), err)
	}
	if strings.TrimSpace(result.Summary) == "" {
		return nil, fmt.Errorf("codex exec result summary is blank")
	}
	result.Enrichments = dropStrippedCodexMemoryCitations(result.Enrichments)
	if err := validateEnrichments(result.Enrichments); err != nil {
		return nil, fmt.Errorf("codex exec result: %w", err)
	}
	result.Effects = normalizeEffects(result.Effects)
	if result.NeedsApproval {
		if result.Outcome != "needs_human" {
			return nil, fmt.Errorf("codex exec needs_approval=true requires outcome=needs_human")
		}
		if result.Proposal == nil {
			return nil, fmt.Errorf("codex exec result needs_approval=true requires a proposal")
		}
		if strings.TrimSpace(result.Proposal.Action) == "" ||
			strings.TrimSpace(result.Proposal.Target) == "" ||
			strings.TrimSpace(result.Proposal.Artifact) == "" {
			return nil, fmt.Errorf("codex exec result proposal must have non-empty action, target and artifact")
		}
	} else if err := validateOutcome(result.Outcome, result.FailureReason, result.NeedsFollowup, result.Waiting); err != nil {
		return nil, fmt.Errorf("codex exec result: %w", err)
	}
	return &result, nil
}

// dropStrippedCodexMemoryCitations removes enrichments that are only Codex
// memory-citation placeholders left blank after Codex strips
// <oai-mem-citation> from --output-last-message. Label text varies ("Memory
// sources", "Memory citation", …); any blank memory_citation is that strip
// artifact. Other blank enrichments are left untouched so validateEnrichments
// still fail-fast on real contract breaks.
func dropStrippedCodexMemoryCitations(items []codexEnrichment) []codexEnrichment {
	if len(items) == 0 {
		return items
	}
	kept := items[:0]
	for _, item := range items {
		if strings.TrimSpace(item.Kind) == "memory_citation" &&
			strings.TrimSpace(item.Content) == "" {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

// normalizeEffects drops only effects that carry no information at all (no kind
// and no fields). It deliberately does NOT validate kind against a whitelist or
// require any specific field: effects are an open, display-only payload, so an
// unknown kind or an effect that only carries extra passthrough fields is kept
// and shown as-is. This is intentionally the opposite of the fail-fast policy
// used for the strict result contract above.
func normalizeEffects(items []codexEffect) []codexEffect {
	if len(items) == 0 {
		return nil
	}
	kept := items[:0]
	for _, item := range items {
		if strings.TrimSpace(item.Kind) == "" &&
			strings.TrimSpace(item.Title) == "" &&
			strings.TrimSpace(item.URL) == "" &&
			strings.TrimSpace(item.Target) == "" &&
			strings.TrimSpace(item.Preview) == "" &&
			len(item.Extra) == 0 {
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func validateEnrichments(items []codexEnrichment) error {
	for position, item := range items {
		if strings.TrimSpace(item.Kind) == "" {
			return fmt.Errorf("enrichments[%d] kind is blank", position)
		}
		if strings.TrimSpace(item.Label) == "" {
			return fmt.Errorf("enrichments[%d] label is blank", position)
		}
		if strings.TrimSpace(item.Content) == "" {
			return fmt.Errorf("enrichments[%d] content is blank", position)
		}
	}
	return nil
}

func validateOutcome(outcome, failureReason, needsFollowup string, waiting *codexWaiting) error {
	switch strings.TrimSpace(outcome) {
	case "completed":
		if waiting != nil {
			return fmt.Errorf("outcome=completed requires waiting=null")
		}
	case "observing":
		if waiting != nil {
			return fmt.Errorf("outcome=observing requires waiting=null")
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

// RunTextSandboxAt is the workspace-rooted counterpart of RunTextSandbox.
// Long-running agents such as the personal daily panorama need a stable
// repository working directory so project Skills, references, and persisted
// Markdown artifacts resolve to the real Jarvis workspace instead of the
// runner's ephemeral temporary directory.
func (r *CodexRunner) RunTextSandboxAt(
	ctx context.Context,
	prompt, sandbox, workspaceRoot string,
) (string, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return "", fmt.Errorf("codex workspace-rooted text run requires a workspace root")
	}
	run, err := r.Run(ctx, prompt, sandbox, workspaceRoot, schemaNone)
	if err != nil {
		return "", err
	}
	return run.LastMessage, nil
}

// RunTextSandboxAtStage is the stage-aware one-shot runner used by autonomous
// system agents. The stage is exported to jarvis-tools for provenance and
// capability boundaries; it does not change the CLI sandbox.
func (r *CodexRunner) RunTextSandboxAtStage(
	ctx context.Context,
	prompt, sandbox, workspaceRoot, agentStage string,
) (string, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return "", fmt.Errorf("codex workspace-rooted text run requires a workspace root")
	}
	run, err := r.run(ctx, prompt, sandbox, workspaceRoot, schemaNone, runInvocation{AgentStage: agentStage})
	if err != nil {
		return "", err
	}
	return run.LastMessage, nil
}

func validAgentStage(stage string) bool {
	if stage == "" {
		return false
	}
	for i, r := range stage {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if i > 0 && (r == '_' || (r >= '0' && r <= '9')) {
			continue
		}
		return false
	}
	return true
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
