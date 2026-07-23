package execute

import (
	"encoding/json"
	"fmt"
	"strings"

	"jarvis/internal/domain"
	"jarvis/internal/sharedmem"
)

// ExecutionPromptVersion identifies the prompt contract for auditing.
const ExecutionPromptVersion = "task-exec-v5"

// maxPriorRunsInPrompt caps how many previous execution_run rows ride into the
// next M5 prompt. Newest runs are kept; older ones are dropped to bound size.
const maxPriorRunsInPrompt = 5

// priorRunSummary is a compact view of one earlier execution_run. It is fed into
// re-run prompts so the agent knows what already happened (side effects, failures,
// artifacts) instead of starting from a blank slate.
type priorRunSummary struct {
	RunID           uint64          `json:"run_id"`
	Status          string          `json:"status"`
	Summary         string          `json:"summary,omitempty"`
	ErrorDetail     string          `json:"error_detail,omitempty"`
	Output          json.RawMessage `json:"output,omitempty"`
	Branch          string          `json:"branch,omitempty"`
	Commit          string          `json:"commit,omitempty"`
	MergeRequestURL string          `json:"merge_request_url,omitempty"`
	StartedAt       string          `json:"started_at"`
	FinishedAt      string          `json:"finished_at,omitempty"`
}

// executionResultSchema is the JSON schema codex MUST return as its final
// message. It distinguishes completion from a durable wait, human input, and
// failure instead of inferring completion from the process exit code.
const executionResultSchema = `{
  "type":"object",
  "additionalProperties":false,
  "required":["outcome","summary","failure_reason","needs_followup","enrichments","waiting"],
  "properties":{
    "outcome":{"type":"string","enum":["completed","waiting","needs_human","failed"]},
    "summary":{"type":"string","minLength":1},
    "failure_reason":{"type":"string"},
    "needs_followup":{"type":"string"},
    "enrichments":{
      "type":"array",
      "items":{
        "type":"object",
        "additionalProperties":false,
        "required":["kind","label","detail"],
        "properties":{
          "kind":{"type":"string","minLength":1},
          "label":{"type":"string","minLength":1},
          "detail":{"type":"string"}
        }
      }
    },
    "waiting":{
      "type":["object","null"],
      "additionalProperties":false,
      "required":["scheduled_task_id","wake_at","reason"],
      "properties":{
        "scheduled_task_id":{"type":"integer","minimum":1},
        "wake_at":{"type":"string"},
        "reason":{"type":"string"}
      }
    }
  }
}`

// proposeResultSchema is the JSON schema codex MUST return for the propose
// stage of an external-side-effect action. The agent first judges risk:
//   - low risk  -> it finishes the work itself and returns needs_approval=false
//     with the normal success verdict.
//   - high-risk external write -> it does NOT touch the outside world; it returns
//     needs_approval=true plus a fully-formed proposal (what it will do, the
//     target object, and the complete artifact) for a human to approve.
const proposeResultSchema = `{
  "type":"object",
  "additionalProperties":false,
  "required":["needs_approval","outcome","summary","failure_reason","needs_followup","enrichments","proposal","waiting"],
  "properties":{
    "needs_approval":{"type":"boolean"},
    "outcome":{"type":"string","enum":["completed","waiting","needs_human","failed"]},
    "summary":{"type":"string","minLength":1},
    "failure_reason":{"type":"string"},
    "needs_followup":{"type":"string"},
    "enrichments":{
      "type":"array",
      "items":{
        "type":"object",
        "additionalProperties":false,
        "required":["kind","label","detail"],
        "properties":{
          "kind":{"type":"string","minLength":1},
          "label":{"type":"string","minLength":1},
          "detail":{"type":"string"}
        }
      }
    },
    "proposal":{
      "type":["object","null"],
      "additionalProperties":false,
      "required":["action","target","artifact"],
      "properties":{
        "action":{"type":"string"},
        "target":{"type":"string"},
        "artifact":{"type":"string"}
      }
    },
    "waiting":{
      "type":["object","null"],
      "additionalProperties":false,
      "required":["scheduled_task_id","wake_at","reason"],
      "properties":{
        "scheduled_task_id":{"type":"integer","minimum":1},
        "wake_at":{"type":"string"},
        "reason":{"type":"string"}
      }
    }
  }
}`

type executionPromptPayload struct {
	PromptVersion        string                `json:"prompt_version"`
	Task                 executionTask         `json:"task"`
	RepoPath             string                `json:"repo_path,omitempty"`
	ExecutionSupplements []ExecutionSupplement `json:"execution_supplements,omitempty"`
	PreviousRuns         []priorRunSummary     `json:"previous_runs,omitempty"`
}

type executionTask struct {
	ID         uint64          `json:"id"`
	Title      string          `json:"title"`
	ActionType string          `json:"action_type"`
	Plan       json.RawMessage `json:"plan"`
	Background json.RawMessage `json:"background"`
}

// buildTaskContext assembles the shared TASK_CONTEXT block (confirmed plan,
// frozen background, repo, M5 execution_supplements, and previous run results)
// that every execution prompt carries. It returns the decoded supplements (for
// the directive block) and the JSON-encoded context. Validation is fail-fast.
func buildTaskContext(task *domain.Task, repoPath string, previousRuns []priorRunSummary) ([]ExecutionSupplement, []byte, error) {
	if task == nil || task.ID == 0 {
		return nil, nil, fmt.Errorf("execution prompt Task is invalid")
	}
	if strings.TrimSpace(task.Title) == "" || strings.TrimSpace(task.ActionType) == "" {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d missing title or action_type", task.ID)
	}
	supplements, err := decodeExecutionSupplements(task.ExecutionSupplements)
	if err != nil {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d execution_supplements invalid: %w", task.ID, err)
	}
	payload := executionPromptPayload{
		PromptVersion:        ExecutionPromptVersion,
		RepoPath:             repoPath,
		ExecutionSupplements: supplements,
		PreviousRuns:         previousRuns,
		Task: executionTask{
			ID: task.ID, Title: task.Title, ActionType: task.ActionType,
			Plan: rawJSON(task.Plan), Background: rawJSON(task.Background),
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("encode execution prompt payload task_id=%d: %w", task.ID, err)
	}
	return supplements, encoded, nil
}

// renderPrompt glues the stage instructions, the shared-memory block, the
// supplement directive block, and the encoded TASK_CONTEXT into the final codex
// prompt. sharedMemory (可信共享记忆) is injected right after the instructions and
// before TASK_CONTEXT（不可信业务数据），即受信任指令区；为空则不注入。
func renderPrompt(instructions, scheduledTools, sharedMemory, workRules, skills string, supplements []ExecutionSupplement, encoded []byte) string {
	directive := formatExecutionSupplementDirective(supplements)
	prompt := strings.TrimSpace(instructions)
	if block := sharedmem.RenderBlock(sharedMemory); block != "" {
		prompt += "\n\n" + block
	}
	if block := strings.TrimSpace(workRules); block != "" {
		prompt += "\n\n" + block
	}
	if block := strings.TrimSpace(skills); block != "" {
		prompt += "\n\n" + block
	}
	if block := strings.TrimSpace(scheduledTools); block != "" {
		prompt += "\n\n" + block
	}
	return prompt + directive +
		"\n\nTASK_CONTEXT_LENGTH_BYTES=" + fmt.Sprintf("%d", len(encoded)) +
		"\nBEGIN_TASK_CONTEXT\n" + string(encoded) + "\nEND_TASK_CONTEXT"
}

// buildExecutionPrompt assembles the agent-driven execution prompt for local
// actions (and low-level use). It does not script the steps; it gives codex the
// confirmed plan, context, and repo, and tells it to carry the plan out. codex
// orchestrates the actual work. task.execution_supplements (M5-only) are injected
// as high-priority directives. previousRuns (if any) carry prior attempt results.
func buildExecutionPrompt(systemPrompt string, task *domain.Task, repoPath, scheduledTools, sharedMemory, workRules, skills string, previousRuns []priorRunSummary) (string, error) {
	systemPrompt = strings.TrimSpace(systemPrompt)
	if systemPrompt == "" {
		return "", fmt.Errorf("execution system prompt is required")
	}
	supplements, encoded, err := buildTaskContext(task, repoPath, previousRuns)
	if err != nil {
		return "", err
	}

	if repoPath != "" {
		systemPrompt += "\n\n当前工作目录已切到 repo：" + repoPath + "，直接在此改动。"
	}

	return renderPrompt(systemPrompt, scheduledTools, sharedMemory, workRules, skills, supplements, encoded), nil
}

// buildProposePrompt assembles the propose-stage prompt. This stage runs for
// every action except code_change, so the task may or may not actually touch the
// outside world — the agent must decide that from what it actually intends to do
// this time, and either finish read-only/local work or produce a full proposal
// WITHOUT touching the outside world. Its final message must satisfy
// proposeResultSchema.
func buildProposePrompt(systemPrompt string, task *domain.Task, scheduledTools, sharedMemory, workRules, skills string, previousRuns []priorRunSummary) (string, error) {
	systemPrompt = strings.TrimSpace(systemPrompt)
	if systemPrompt == "" {
		return "", fmt.Errorf("propose system prompt is required")
	}
	supplements, encoded, err := buildTaskContext(task, "", previousRuns)
	if err != nil {
		return "", err
	}

	return renderPrompt(systemPrompt, scheduledTools, sharedMemory, workRules, skills, supplements, encoded), nil
}

// buildApplyPrompt assembles the apply-stage prompt after a human approved a
// proposal. The approved plan + full artifact is embedded verbatim and codex is
// told to land it faithfully for real. Its final message must satisfy
// executionResultSchema.
func buildApplyPrompt(systemPrompt string, task *domain.Task, proposal *codexProposal, approvalRule, scheduledTools, sharedMemory, workRules, skills string, previousRuns []priorRunSummary) (string, error) {
	systemPrompt = strings.TrimSpace(systemPrompt)
	if systemPrompt == "" {
		return "", fmt.Errorf("apply system prompt is required")
	}
	if proposal == nil {
		return "", fmt.Errorf("apply prompt Task id=%d has no approved proposal", task.ID)
	}
	approvalRule = strings.TrimSpace(approvalRule)
	if approvalRule == "" {
		return "", fmt.Errorf("apply prompt Task id=%d approval rule is required", task.ID)
	}
	supplements, encoded, err := buildTaskContext(task, "", previousRuns)
	if err != nil {
		return "", err
	}
	approved, err := json.Marshal(map[string]string{
		"action":   proposal.Action,
		"target":   proposal.Target,
		"artifact": proposal.Artifact,
	})
	if err != nil {
		return "", fmt.Errorf("encode approved proposal task_id=%d: %w", task.ID, err)
	}

	instructions := systemPrompt + `

BEGIN_APPROVAL_RULE（这是委托人在后台明确维护的可信审批规则，必须遵守。）
` + approvalRule + `
END_APPROVAL_RULE

APPROVED_PROPOSAL=` + string(approved)

	return renderPrompt(instructions, scheduledTools, sharedMemory, workRules, skills, supplements, encoded), nil
}
