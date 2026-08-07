// Package codexengine is the M3 extraction engine backed by the codex CLI.
//
// Unlike the model API engine (an application-level function-calling loop),
// codex self-runs shell (jarvis-tools/lark-cli/bytedcli/git) inside its own
// process to collect decisive Task-admission facts before emitting the result.
// It must stop once it can admit, observe, or drop the clue; execution planning
// remains M5's job. We therefore do NOT implement a Go tool loop here — we send
// one prompt and read one structured JSON response (codex enforces
// --output-schema).
//
// This engine implements the same method the worker calls on the kimi client
// (ExtractWithTools) so it drops into extract.NewWorker unchanged. The ToolBox
// and maxRounds arguments are intentionally ignored: codex owns its own tools.
package codexengine

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
	"time"

	"jarvis/internal/agentusage"
	"jarvis/internal/extract"
	"jarvis/internal/extract/provider"
)

const maxCodexOutputBytes = 1 << 20

// Options configures the codex admission engine. Sandbox/Network/ReasoningEffort
// come from config; in the local trusted environment sandbox is
// danger-full-access with network on, while the prompt owns the no-write M3
// boundary.
type Options struct {
	Bin             string
	Model           string
	Sandbox         string
	Network         bool
	ReasoningEffort string
	Timeout         time.Duration
}

// Extractor runs codex once per unit to produce the todo_extraction JSON.
type Extractor struct {
	bin             string
	model           string
	sandbox         string
	network         bool
	reasoningEffort string
	timeout         time.Duration
}

func New(opts Options) (*Extractor, error) {
	if strings.TrimSpace(opts.Bin) == "" {
		return nil, fmt.Errorf("codex extractor bin is required")
	}
	bin, err := exec.LookPath(opts.Bin)
	if err != nil {
		return nil, fmt.Errorf("find codex extractor binary %q: %w", opts.Bin, err)
	}
	if strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf("codex extractor model is required")
	}
	switch opts.Sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("codex extractor sandbox %q is invalid", opts.Sandbox)
	}
	if strings.TrimSpace(opts.ReasoningEffort) == "" {
		return nil, fmt.Errorf("codex extractor reasoning_effort is required")
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("codex extractor timeout must be positive")
	}
	return &Extractor{
		bin: bin, model: opts.Model, sandbox: opts.Sandbox, network: opts.Network,
		reasoningEffort: opts.ReasoningEffort, timeout: opts.Timeout,
	}, nil
}

// ExtractWithTools satisfies the worker's model transport. box is ignored
// (codex self-runs its own tools). It sends system+user as one prompt and
// decodes codex's schema-constrained JSON via the same strict decoder the kimi
// path uses, so downstream validation is identical across engines.
func (e *Extractor) ExtractWithTools(ctx context.Context, prompt extract.Prompt, _ extract.ToolBox) (*extract.ExtractionResult, error) {
	combined := strings.TrimSpace(prompt.System + "\n\n" + prompt.User)
	if combined == "" {
		return nil, fmt.Errorf("codex extraction prompt is empty")
	}

	tempDir, err := os.MkdirTemp("", "jarvis-codex-extract-")
	if err != nil {
		return nil, fmt.Errorf("create codex extraction temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	schemaPath := filepath.Join(tempDir, "todo.schema.json")
	resultPath := filepath.Join(tempDir, "todo.json")
	schemaBytes, err := json.Marshal(provider.TodoExtractionJSONSchema())
	if err != nil {
		return nil, fmt.Errorf("encode todo extraction schema: %w", err)
	}
	if err := os.WriteFile(schemaPath, schemaBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write todo extraction schema: %w", err)
	}

	args := []string{
		"exec", "--ephemeral", "--sandbox", e.sandbox, "--color", "never", "--json",
		"--output-schema", schemaPath, "--output-last-message", resultPath,
		"--model", e.model, "-c", "model_reasoning_effort=" + e.reasoningEffort,
	}
	if e.network && e.sandbox == "workspace-write" {
		// workspace-write disables network by default; explicitly re-enable it so
		// codex can reach lark-cli/bytedcli endpoints. danger-full-access already
		// has network, so this is only needed for the narrower sandbox.
		args = append(args, "-c", "sandbox_workspace_write.network_access=true")
	}
	args = append(args, "--skip-git-repo-check", "-")

	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, e.bin, args...)
	command.Env = append(os.Environ(), "JARVIS_AGENT_STAGE=extract")
	command.Dir = tempDir
	command.Stdin = strings.NewReader(combined)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	commandErr := command.Run()
	usage, usageErr := agentusage.ParseCodexJSONL(stdout.Bytes())
	if usageErr == nil {
		if err := agentusage.Record(ctx, usage); err != nil {
			return nil, fmt.Errorf("record codex extraction usage: %w", err)
		}
	}
	if commandErr != nil {
		var runErr error
		if runCtx.Err() == context.DeadlineExceeded {
			runErr = fmt.Errorf("codex extraction timed out after %s", e.timeout)
		} else {
			runErr = fmt.Errorf("codex extraction failed: %w: %s", commandErr, limitedText(stderr.Bytes(), 4096))
		}
		if usageErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("parse failed codex extraction usage: %w", usageErr))
		}
		return nil, runErr
	}
	if usageErr != nil {
		return nil, fmt.Errorf("parse codex extraction usage: %w", usageErr)
	}

	resultBytes, err := readLimitedFile(resultPath, maxCodexOutputBytes)
	if err != nil {
		return nil, err
	}
	result, err := extract.DecodeExtractionResult(resultBytes)
	if err != nil {
		return nil, fmt.Errorf("decode codex extraction result: %w", err)
	}
	return result, nil
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open codex extraction result: %w", err)
	}
	defer file.Close()
	result, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read codex extraction result: %w", err)
	}
	if int64(len(result)) > limit {
		return nil, fmt.Errorf("codex extraction result exceeds %d bytes", limit)
	}
	return result, nil
}

func limitedText(b []byte, limit int) string {
	b = bytes.TrimSpace(b)
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "...(truncated)"
}
