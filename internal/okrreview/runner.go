// Package okrreview runs the advisory OKR Preview review as a single
// short-lived agent call.
//
// It deliberately does not go through internal/execute: a review forms an
// opinion and writes nothing, so it needs neither a Task row, an ExecutionRun,
// the approval state machine, nor the M5 prompt stack. What it does need is the
// module's own read tools, so the agent can follow alignment and related KRs
// beyond the object under review.
package okrreview

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// runner invokes the review CLI once and returns everything the agent said.
type runner struct {
	bin             string
	model           string
	sandbox         string
	reasoningEffort string
	timeout         time.Duration
}

func newRunner(bin, model, sandbox, reasoningEffort string, timeout time.Duration) (*runner, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, fmt.Errorf("okr review bin is required")
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("find okr review binary %q: %w", bin, err)
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("okr review model is required")
	}
	switch sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("okr review sandbox must be read-only, workspace-write or danger-full-access, got %q", sandbox)
	}
	if strings.TrimSpace(reasoningEffort) == "" {
		return nil, fmt.Errorf("okr review reasoning_effort is required")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("okr review timeout must be positive")
	}
	return &runner{bin: resolved, model: model, sandbox: sandbox, reasoningEffort: reasoningEffort, timeout: timeout}, nil
}

// args builds the one-shot invocation. Network access is on because the review
// tools reach the local Jarvis API; JARVIS_API_BASE and the scripts directory
// are already exported into this process by config.ExportToolEnvironment, so the
// child inherits them and never has to resolve the instance itself.
func (r *runner) args() []string {
	return []string{
		"exec", "--json", "--color", "never",
		"--sandbox", r.sandbox,
		"-c", "sandbox_workspace_write.network_access=true",
		"--skip-git-repo-check",
		"-c", "model_reasoning_effort=" + r.reasoningEffort,
		"--model", r.model,
		"-",
	}
}

// Run feeds prompt on stdin and returns the agent's prose. fail-fast: timeout,
// non-zero exit, malformed JSONL and an empty answer are all errors.
func (r *runner) Run(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("okr review prompt is required")
	}
	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	command := exec.CommandContext(runCtx, r.bin, r.args()...)
	command.Env = append(os.Environ(), "JARVIS_AGENT_STAGE=okr_review")
	command.Stdin = strings.NewReader(prompt)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("open okr review stdout pipe: %w", err)
	}
	var stderr stderrCollector
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start okr review agent: %w", err)
	}
	message, parseErr := collectAgentMessage(stdout)
	waitErr := command.Wait()

	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("okr review timed out after %s: %s", r.timeout, stderr.text())
	}
	if waitErr != nil {
		return "", fmt.Errorf("okr review agent exited abnormally: %w: %s", waitErr, stderr.text())
	}
	if parseErr != nil {
		return "", parseErr
	}
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("okr review agent produced no review text: %s", stderr.text())
	}
	return message, nil
}

// collectAgentMessage reads the CLI's JSONL stream and keeps only what the agent
// said to the reader. A run also emits reasoning and command_execution items;
// those are its working notes, not the review.
//
// A decoder rather than a line scanner: one captured command output easily
// exceeds any fixed line buffer, and find-krs alone returns over a thousand lines.
func collectAgentMessage(stdout io.Reader) (string, error) {
	decoder := json.NewDecoder(bufio.NewReader(stdout))
	var parts []string
	for {
		var event agentEvent
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("decode okr review JSONL stream: %w", err)
		}
		if event.Type != "item.completed" || event.Item == nil || event.Item.Type != "agent_message" {
			continue
		}
		if text := strings.TrimSpace(event.Item.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

type agentEvent struct {
	Type string     `json:"type"`
	Item *agentItem `json:"item"`
}

type agentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// stderrCollector keeps a bounded head of the CLI's stderr for diagnostics.
type stderrCollector struct {
	mu  sync.Mutex
	buf []byte
}

const maxStderrBytes = 8192

func (s *stderrCollector) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if room := maxStderrBytes - len(s.buf); room > 0 {
		if len(p) <= room {
			s.buf = append(s.buf, p...)
		} else {
			s.buf = append(s.buf, p[:room]...)
		}
	}
	return len(p), nil
}

func (s *stderrCollector) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := strings.TrimSpace(string(s.buf)); t != "" {
		return t
	}
	return "(no stderr)"
}
