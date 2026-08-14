package factengine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxExtractorOutputBytes = 1 << 20

// ExtractorOptions configures the Agent CLI used by world maintenance and the
// daily rollup. Each caller owns its own Extractor so their models can differ.
type ExtractorOptions struct {
	Bin             string
	Model           string
	ReasoningEffort string
	Sandbox         string
	WorkspaceRoot   string
	Timeout         time.Duration
}

// Extractor runs a complete ephemeral Agent session. World maintenance writes
// through tools and returns an audit message; no semantic output is parsed by Go.
type Extractor struct {
	bin             string
	model           string
	reasoningEffort string
	sandbox         string
	root            string
	timeout         time.Duration
}

func NewExtractor(opts ExtractorOptions) (*Extractor, error) {
	if strings.TrimSpace(opts.Bin) == "" {
		return nil, fmt.Errorf("fact extractor bin is required")
	}
	bin, err := exec.LookPath(opts.Bin)
	if err != nil {
		return nil, fmt.Errorf("find fact extractor binary %q: %w", opts.Bin, err)
	}
	if strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf("fact extractor model is required")
	}
	reasoningEffort := strings.TrimSpace(opts.ReasoningEffort)
	if reasoningEffort != "" {
		switch reasoningEffort {
		case "minimal", "low", "medium", "high", "xhigh":
		default:
			return nil, fmt.Errorf("fact extractor reasoning effort %q is invalid", reasoningEffort)
		}
	}
	switch opts.Sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("fact extractor sandbox %q is invalid", opts.Sandbox)
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("fact extractor timeout must be positive")
	}
	root := strings.TrimSpace(opts.WorkspaceRoot)
	if root == "" {
		return nil, fmt.Errorf("fact extractor workspace root is required")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve fact extractor workspace root %q: %w", opts.WorkspaceRoot, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat fact extractor workspace root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("fact extractor workspace root %q is not a directory", root)
	}
	return &Extractor{
		bin: bin, model: opts.Model, reasoningEffort: reasoningEffort,
		sandbox: opts.Sandbox, root: root, timeout: opts.Timeout,
	}, nil
}

// Maintain runs one world-maintenance batch. The Agent performs every durable
// write with Jarvis tools; a non-empty final message is retained only for logs.
func (e *Extractor) Maintain(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return "", fmt.Errorf("factengine system prompt is empty")
	}
	if strings.TrimSpace(userPrompt) == "" {
		return "", fmt.Errorf("factengine material prompt is empty")
	}
	raw, err := e.run(ctx, "world-maintenance", systemPrompt+"\n\n"+userPrompt)
	if err != nil {
		return "", err
	}
	result := strings.TrimSpace(string(raw))
	if result == "" {
		return "", fmt.Errorf("factengine Agent returned an empty final message")
	}
	return result, nil
}

func (e *Extractor) run(ctx context.Context, label, prompt string) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "jarvis-fact-agent-")
	if err != nil {
		return nil, fmt.Errorf("create fact Agent temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	resultPath := filepath.Join(tempDir, "last-message.txt")

	args := []string{
		"exec", "--ephemeral", "--sandbox", e.sandbox, "--color", "never",
		"--output-last-message", resultPath,
		"--model", e.model,
	}
	if e.reasoningEffort != "" {
		args = append(args, "-c", "model_reasoning_effort="+e.reasoningEffort)
	}
	args = append(args, "--skip-git-repo-check", "-")
	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, e.bin, args...)
	command.Env = append(os.Environ(), "JARVIS_AGENT_STAGE=factengine")
	command.Dir = e.root
	command.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("fact Agent %s timed out after %s", label, e.timeout)
		}
		return nil, fmt.Errorf("fact Agent %s prompt_chars=%d failed: %w: %s", label, len(prompt), err, limitedTail(stderr.Bytes(), 4096))
	}
	return readLimitedFile(resultPath, maxExtractorOutputBytes)
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open fact extraction result: %w", err)
	}
	defer file.Close()
	result, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read fact extraction result: %w", err)
	}
	if int64(len(result)) > limit {
		return nil, fmt.Errorf("fact extraction result exceeds %d bytes", limit)
	}
	return result, nil
}

// Agent CLIs print the whole prompt before their actual failure. Error paths
// need the tail, otherwise a context-limit/provider error is hidden behind the
// first few kilobytes of echoed input.
func limitedTail(b []byte, limit int) string {
	b = bytes.TrimSpace(b)
	if len(b) <= limit {
		return string(b)
	}
	return "...(truncated)" + string(b[len(b)-limit:])
}
