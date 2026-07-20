package decide

import (
	"bufio"
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
	maxCodexOutputBytes = 1 << 20
	codexDecisionSchema = `{
  "type":"object",
  "additionalProperties":false,
  "required":["confidence_factors","risk_factors","confidence_basis","clarifications","recommended_review","proposed_plan","plan_is_clear"],
  "properties":{
    "confidence_factors":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/factor"}},
    "risk_factors":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/factor"}},
    "confidence_basis":{"type":"string","minLength":1},
    "clarifications":{"type":"array","items":{"$ref":"#/$defs/clarification"}},
    "recommended_review":{"type":"boolean"},
    "proposed_plan":{"anyOf":[{"$ref":"#/$defs/plan"},{"type":"null"}]},
    "plan_is_clear":{"type":"boolean"}
  },
  "$defs":{
    "factor":{
      "type":"object","additionalProperties":false,
      "required":["name","score","basis"],
      "properties":{"name":{"type":"string","minLength":1},"score":{"type":"number","minimum":0,"maximum":1},"basis":{"type":"string","minLength":1}}
    },
    "clarification":{
      "type":"object","additionalProperties":false,
      "required":["question","hint"],
      "properties":{"question":{"type":"string","minLength":1},"hint":{"type":"string"}}
    },
    "parameter":{
      "type":"object","additionalProperties":false,
      "required":["name","value"],
      "properties":{"name":{"type":"string","minLength":1},"value":{"type":"string"}}
    },
    "plan":{
      "type":"object","additionalProperties":false,
      "required":["summary","steps","parameters","basis"],
      "properties":{
        "summary":{"type":"string","minLength":1},
        "steps":{"type":"array","minItems":1,"items":{"type":"string","minLength":1}},
        "parameters":{"type":"array","items":{"$ref":"#/$defs/parameter"}},
        "basis":{"type":"array","items":{"type":"string","minLength":1}}
      }
    }
  }
}`
)

type CodexOptions struct {
	Bin     string
	Model   string
	Timeout time.Duration
	// Sandbox / Network / ReasoningEffort配置 M4 决策 codex 的权限与推理档位。
	// 设计 §2.3：M4 决策也可自查补信息，用 danger-full-access + 联网 + low 推理。
	Sandbox         string
	Network         bool
	ReasoningEffort string
}

type CodexInput struct {
	Prompt   string
	RepoPath string
}

type CodexDecision struct {
	ConfidenceFactors []DecisionFactor `json:"confidence_factors"`
	RiskFactors       []DecisionFactor `json:"risk_factors"`
	ConfidenceBasis   string           `json:"confidence_basis"`
	// Clarifications are the points Codex needs the human to clarify or supply:
	// missing info when the plan is not clear (need_info), or uncertainties it
	// wants a human to decide on (need_review). Meaning is defined in the prompt,
	// not the struct — keep it loose on purpose.
	Clarifications    []Clarification `json:"clarifications"`
	RecommendedReview bool            `json:"recommended_review"`
	ProposedPlan      *PlanDraft      `json:"proposed_plan"`
	PlanIsClear       bool            `json:"plan_is_clear"`
}

// Clarification is one thing Codex asks the human to clarify or provide. Both
// fields are free text; the prompt defines what to put there.
type Clarification struct {
	Question string `json:"question"`       // 要澄清/需要补充的点
	Hint     string `json:"hint,omitempty"` // 可选：给填写者的提示或示例
}

type DecisionFactor struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
	Basis string  `json:"basis"`
}

type PlanDraft struct {
	Summary    string          `json:"summary"`
	Steps      []string        `json:"steps"`
	Parameters []PlanParameter `json:"parameters"`
	Basis      []string        `json:"basis"`
}

type PlanParameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
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

func codexSessionID(output []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 4096), maxCodexOutputBytes)
	var sessionID string
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", fmt.Errorf("decode codex JSONL event line=%d: %w", lineNumber, err)
		}
		if event.Type == "thread.started" {
			if strings.TrimSpace(event.ThreadID) == "" {
				return "", fmt.Errorf("codex thread.started event is missing thread_id")
			}
			sessionID = event.ThreadID
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan codex JSONL output: %w", err)
	}
	if sessionID == "" {
		return "", fmt.Errorf("codex JSONL output is missing thread.started event")
	}
	return sessionID, nil
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open codex decision result: %w", err)
	}
	defer file.Close()
	result, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read codex decision result: %w", err)
	}
	if int64(len(result)) > limit {
		return nil, fmt.Errorf("codex decision result exceeds %d bytes", limit)
	}
	return result, nil
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
	if err := validateFactors("confidence", decision.ConfidenceFactors); err != nil {
		return nil, err
	}
	if err := validateFactors("risk", decision.RiskFactors); err != nil {
		return nil, err
	}
	if strings.TrimSpace(decision.ConfidenceBasis) == "" {
		return nil, fmt.Errorf("codex decision confidence_basis is blank")
	}
	for position, clarification := range decision.Clarifications {
		if strings.TrimSpace(clarification.Question) == "" {
			return nil, fmt.Errorf("codex decision clarifications[%d] question is blank", position)
		}
	}
	// A Todo that is not clear enough to act (need_info) must tell the human what
	// to clarify — otherwise "需要补充信息" is useless. Fail-fast on an empty list.
	if !decision.PlanIsClear && len(decision.Clarifications) == 0 {
		return nil, fmt.Errorf("codex decision plan_is_clear=false requires at least one clarification")
	}
	if decision.PlanIsClear && decision.ProposedPlan == nil {
		return nil, fmt.Errorf("codex decision plan_is_clear requires proposed_plan")
	}
	if decision.ProposedPlan != nil {
		if err := validatePlanDraft(decision.ProposedPlan); err != nil {
			return nil, err
		}
	}
	return &decision, nil
}

func validateFactors(name string, factors []DecisionFactor) error {
	if len(factors) == 0 {
		return fmt.Errorf("codex decision %s_factors is empty", name)
	}
	seen := make(map[string]struct{}, len(factors))
	for position, factor := range factors {
		factorName := strings.TrimSpace(factor.Name)
		if factorName == "" {
			return fmt.Errorf("codex decision %s_factors[%d] has blank name", name, position)
		}
		if _, exists := seen[factorName]; exists {
			return fmt.Errorf("codex decision %s_factors contains duplicate name %q", name, factorName)
		}
		seen[factorName] = struct{}{}
		if !unitScore(factor.Score) {
			return fmt.Errorf("codex decision %s factor %q score=%v is outside [0,1]", name, factorName, factor.Score)
		}
		if strings.TrimSpace(factor.Basis) == "" {
			return fmt.Errorf("codex decision %s factor %q basis is blank", name, factorName)
		}
	}
	return nil
}

func validatePlanDraft(plan *PlanDraft) error {
	if strings.TrimSpace(plan.Summary) == "" {
		return fmt.Errorf("codex proposed_plan summary is blank")
	}
	if len(plan.Steps) == 0 {
		return fmt.Errorf("codex proposed_plan steps is empty")
	}
	for position, step := range plan.Steps {
		if strings.TrimSpace(step) == "" {
			return fmt.Errorf("codex proposed_plan steps[%d] is blank", position)
		}
	}
	seen := make(map[string]struct{}, len(plan.Parameters))
	for position, parameter := range plan.Parameters {
		name := strings.TrimSpace(parameter.Name)
		if name == "" {
			return fmt.Errorf("codex proposed_plan parameters[%d] has blank name", position)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("codex proposed_plan contains duplicate parameter %q", name)
		}
		seen[name] = struct{}{}
	}
	for position, basis := range plan.Basis {
		if strings.TrimSpace(basis) == "" {
			return fmt.Errorf("codex proposed_plan basis[%d] is blank", position)
		}
	}
	return nil
}

func limitedText(value []byte, limit int) string {
	value = bytes.TrimSpace(value)
	if len(value) <= limit {
		return string(value)
	}
	return string(value[:limit]) + "..."
}
