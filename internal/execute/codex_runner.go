package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxCodexOutputBytes = 1 << 20

// codexRun is the raw outcome of one codex CLI invocation.
type codexRun struct {
	SessionID   string
	LastMessage string
}

// CodexRunner wraps the codex CLI for M5 execution. Unlike the M4 decider (which
// is always read-only), the runner picks the sandbox per task and never uses
// danger-full-access or approval bypass. In `codex exec` the sandbox flag is the
// enforcement boundary, so workspace-write cannot touch anything outside the repo.
type CodexRunner struct {
	bin     string
	model   string
	timeout time.Duration
}

func NewCodexRunner(bin, model string, timeout time.Duration) (*CodexRunner, error) {
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
	if timeout <= 0 {
		return nil, fmt.Errorf("codex runner timeout must be positive")
	}
	return &CodexRunner{bin: resolved, model: model, timeout: timeout}, nil
}

// Run executes codex once with the given prompt and sandbox. sandbox must be
// "read-only" or "workspace-write". repoPath, when non-empty, is the working
// directory codex operates in (required for workspace-write / code changes).
func (r *CodexRunner) Run(ctx context.Context, prompt, sandbox, repoPath string) (*codexRun, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("codex run prompt is required")
	}
	if sandbox != "read-only" && sandbox != "workspace-write" {
		return nil, fmt.Errorf("codex run sandbox must be read-only or workspace-write, got %q", sandbox)
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
	args := []string{
		"exec", "--ephemeral", "--sandbox", sandbox,
		"--color", "never", "--json", "--output-last-message", resultPath, "--model", r.model,
	}
	if strings.TrimSpace(repoPath) != "" {
		args = append(args, "--cd", repoPath)
	} else {
		args = append(args, "--skip-git-repo-check")
	}
	args = append(args, "-")

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, r.bin, args...)
	if strings.TrimSpace(repoPath) == "" {
		command.Dir = tempDir
	}
	command.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
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
	return &codexRun{SessionID: sessionID, LastMessage: string(bytes.TrimSpace(last))}, nil
}

// RunText runs codex read-only and returns just the final message text. It is
// used by lightweight callers (e.g. the Progress digest summarizer) that only
// need free-form prose, not the M5 execution run bookkeeping.
func (r *CodexRunner) RunText(ctx context.Context, prompt string) (string, error) {
	run, err := r.Run(ctx, prompt, "read-only", "")
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
