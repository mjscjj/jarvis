package okrtranslation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxCodexTranslationBytes = 4 << 20

type CodexOptions struct {
	Bin             string
	Model           string
	Sandbox         string
	ReasoningEffort string
	Timeout         time.Duration
}

type codexCompleter struct {
	bin             string
	model           string
	sandbox         string
	reasoningEffort string
	timeout         time.Duration
}

// NewCodexCompleter reuses the module's existing one-shot agent runtime. It
// requires no Jarvis tools: the complete source batch is already in the prompt.
func NewCodexCompleter(opts CodexOptions) (*codexCompleter, error) {
	bin, err := exec.LookPath(strings.TrimSpace(opts.Bin))
	if err != nil {
		return nil, fmt.Errorf("find OKR translation binary %q: %w", opts.Bin, err)
	}
	if strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf("OKR translation model is required")
	}
	switch opts.Sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("OKR translation sandbox %q is invalid", opts.Sandbox)
	}
	if strings.TrimSpace(opts.ReasoningEffort) == "" {
		return nil, fmt.Errorf("OKR translation reasoning_effort is required")
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("OKR translation timeout must be positive")
	}
	return &codexCompleter{bin: bin, model: opts.Model, sandbox: opts.Sandbox, reasoningEffort: opts.ReasoningEffort, timeout: opts.Timeout}, nil
}

func (c *codexCompleter) CompleteStructured(ctx context.Context, operation, schemaName string, schema map[string]any, system, user string) ([]byte, error) {
	if strings.TrimSpace(system) == "" || strings.TrimSpace(user) == "" {
		return nil, fmt.Errorf("%s prompt is empty", operation)
	}
	tempDir, err := os.MkdirTemp("", "jarvis-okr-translation-")
	if err != nil {
		return nil, fmt.Errorf("create OKR translation temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	schemaPath := filepath.Join(tempDir, schemaName+".schema.json")
	resultPath := filepath.Join(tempDir, schemaName+".json")
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode %s schema: %w", operation, err)
	}
	if err := os.WriteFile(schemaPath, schemaBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write %s schema: %w", operation, err)
	}

	args := []string{
		"exec", "--ephemeral", "--sandbox", c.sandbox, "--color", "never", "--json",
		"--output-schema", schemaPath, "--output-last-message", resultPath,
		"--model", c.model, "-c", "model_reasoning_effort=" + c.reasoningEffort,
		"--skip-git-repo-check", "-",
	}
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, c.bin, args...)
	command.Dir = tempDir
	command.Env = append(os.Environ(), "JARVIS_AGENT_STAGE=okr_translation")
	command.Stdin = strings.NewReader(system + "\n\nReturn only the requested JSON object.\n\nINPUT_JSON\n" + user)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("%s timed out after %s", operation, c.timeout)
		}
		return nil, fmt.Errorf("%s agent failed: %w: %s", operation, err, limited(stderr.String(), 2048))
	}
	info, err := os.Stat(resultPath)
	if err != nil {
		return nil, fmt.Errorf("read %s result metadata: %w", operation, err)
	}
	if info.Size() <= 0 || info.Size() > maxCodexTranslationBytes {
		return nil, fmt.Errorf("%s result size %d is invalid", operation, info.Size())
	}
	result, err := os.ReadFile(resultPath)
	if err != nil {
		return nil, fmt.Errorf("read %s result: %w", operation, err)
	}
	return result, nil
}

func limited(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
