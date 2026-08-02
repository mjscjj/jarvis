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
	"time"
)

const (
	codexDecisionSchema = `{
  "type":"object",
  "additionalProperties":false,
  "required":["disposition","plan","payload"],
  "properties":{
    "disposition":{"type":"string","enum":["ready","drop"]},
    "plan":{"type":"string","minLength":1},
    "payload":{"type":"string","minLength":1}
  }
}`
)

type CodexOptions struct {
	Bin     string
	Model   string
	Timeout time.Duration
	// Sandbox / Network / ReasoningEffort配置 M5 判断环节 codex 的权限与推理档位。
	// 设计 §2.3：M5 判断环节也可自查补信息，用 danger-full-access + 联网 + low 推理。
	Sandbox         string
	Network         bool
	ReasoningEffort string
}

type CodexInput struct {
	Prompt   string
	RepoPath string
}

type CodexDecision struct {
	// Disposition is Codex's own verdict on whether the clue is worth pursuing:
	// ready / drop. It is authoritative — the route is derived from it directly,
	// not re-inferred from semantic payload fields.
	Disposition string `json:"disposition"`
	// Plan is the complete execution intent. The decision step does not prescribe
	// its semantic shape; ready only requires a non-null JSON value.
	Plan json.RawMessage `json:"plan"`
	// Payload carries reasoning, evidence, risks, open questions for the principal
	// and any future model-authored semantics without widening this Go contract.
	Payload json.RawMessage `json:"payload"`
}

type CodexResult struct {
	Decision  CodexDecision `json:"decision"`
	SessionID string        `json:"session_id"`
}

type CodexDecider struct {
	bin             string
	model           string
	timeout         time.Duration
	sandbox         string
	network         bool
	reasoningEffort string
}

func NewCodexDecider(opts CodexOptions) (*CodexDecider, error) {
	if strings.TrimSpace(opts.Bin) == "" {
		return nil, fmt.Errorf("codex decider bin is required")
	}
	bin, err := exec.LookPath(opts.Bin)
	if err != nil {
		return nil, fmt.Errorf("find codex decider binary %q: %w", opts.Bin, err)
	}
	if strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf("codex decider model is required")
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("codex decider timeout must be positive")
	}
	switch opts.Sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("codex decider sandbox %q is invalid", opts.Sandbox)
	}
	if strings.TrimSpace(opts.ReasoningEffort) == "" {
		return nil, fmt.Errorf("codex decider reasoning_effort is required")
	}
	return &CodexDecider{
		bin: bin, model: opts.Model, timeout: opts.Timeout,
		sandbox: opts.Sandbox, network: opts.Network, reasoningEffort: opts.ReasoningEffort,
	}, nil
}

func (d *CodexDecider) Decide(ctx context.Context, input CodexInput) (*CodexResult, error) {
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("codex decision prompt is required")
	}
	repoPath, err := validateRepoPath(input.RepoPath)
	if err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "jarvis-codex-decide-")
	if err != nil {
		return nil, fmt.Errorf("create codex decision temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	schemaPath := filepath.Join(tempDir, "decision.schema.json")
	resultPath := filepath.Join(tempDir, "decision.json")
	if err := os.WriteFile(schemaPath, []byte(codexDecisionSchema), 0o600); err != nil {
		return nil, fmt.Errorf("write codex decision schema: %w", err)
	}

	args := []string{
		"exec", "--ephemeral", "--sandbox", d.sandbox, "--color", "never", "--json",
		"--output-schema", schemaPath, "--output-last-message", resultPath, "--model", d.model,
		"-c", "model_reasoning_effort=" + d.reasoningEffort,
	}
	if d.network && d.sandbox == "workspace-write" {
		// workspace-write disables network by default; re-enable it so codex can
		// self-query lark-cli/bytedcli. danger-full-access already has network.
		args = append(args, "-c", "sandbox_workspace_write.network_access=true")
	}
	if repoPath != "" {
		args = append(args, "--cd", repoPath)
	} else {
		args = append(args, "--skip-git-repo-check")
	}
	args = append(args, "-")
	runCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, d.bin, args...)
	if repoPath == "" {
		command.Dir = tempDir
	}
	command.Stdin = strings.NewReader(prompt)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("codex decision timed out after %s: %w", d.timeout, runCtx.Err())
		}
		return nil, fmt.Errorf("codex decision command failed: %w: %s", err, limitedText(stderr.Bytes(), 4096))
	}
	sessionID, err := codexSessionID(stdout.Bytes())
	if err != nil {
		return nil, err
	}
	resultBytes, err := readLimitedFile(resultPath, maxCodexOutputBytes)
	if err != nil {
		return nil, err
	}
	decision, err := decodeCodexDecision(resultBytes)
	if err != nil {
		return nil, err
	}
	return &CodexResult{Decision: *decision, SessionID: sessionID}, nil
}

func validateRepoPath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve codex repo path %q: %w", value, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("stat codex repo path %q: %w", absolute, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("codex repo path is not a directory: %q", absolute)
	}
	return absolute, nil
}

func decodeCodexDecision(raw []byte) (*CodexDecision, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decision CodexDecision
	if err := decoder.Decode(&decision); err != nil {
		return nil, fmt.Errorf("decode codex decision result: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("codex decision result contains multiple JSON values")
		}
		return nil, fmt.Errorf("decode trailing codex decision result: %w", err)
	}
	payload, err := canonicalJSONValue(decision.Payload, "codex decision payload", false)
	if err != nil {
		return nil, err
	}
	decision.Payload = payload
	switch decision.Disposition {
	case DispositionReady:
		plan, err := canonicalJSONValue(decision.Plan, "codex decision plan", false)
		if err != nil {
			return nil, fmt.Errorf("codex decision disposition=%s requires plan: %w", decision.Disposition, err)
		}
		decision.Plan = plan
	case DispositionDrop:
		if bytes.Equal(bytes.TrimSpace(decision.Plan), []byte("null")) {
			decision.Plan = nil
		} else if len(bytes.TrimSpace(decision.Plan)) != 0 {
			plan, err := canonicalJSONValue(decision.Plan, "codex decision plan", false)
			if err != nil {
				return nil, err
			}
			decision.Plan = plan
		}
	default:
		return nil, fmt.Errorf("codex decision has invalid disposition %q", decision.Disposition)
	}
	return &decision, nil
}
