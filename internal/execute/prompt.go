package execute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/prompttemplate"
	"jarvis/internal/sharedmem"
)

// ExecutionPromptVersion identifies the prompt contract for auditing.
const ExecutionPromptVersion = "task-exec-v12-user-message"

// maxPriorRunsInPrompt caps how many previous execution_run rows ride into the
// next M5 prompt. Newest runs are kept; older ones are dropped to bound size.
const maxPriorRunsInPrompt = 5

const (
	m5PhaseExecute = `BEGIN_M5_PHASE
phase=execute
END_M5_PHASE`
	m5PhaseApply = `BEGIN_M5_PHASE
phase=apply
END_M5_PHASE`
	m5PhaseResumeWaiting = `BEGIN_M5_PHASE
phase=resume_waiting
END_M5_PHASE`
	m5PhaseResumeHuman = `BEGIN_M5_PHASE
phase=resume_human
END_M5_PHASE`
)

// priorRunSummary is a compact view of one earlier execution_run. It is fed into
// re-run prompts so the agent knows what already happened (side effects, failures,
// artifacts) instead of starting from a blank slate.
type priorRunSummary struct {
	RunID       uint64          `json:"run_id"`
	Status      string          `json:"status"`
	Summary     string          `json:"summary,omitempty"`
	ErrorDetail string          `json:"error_detail,omitempty"`
	Output      json.RawMessage `json:"output,omitempty"`
	StartedAt   string          `json:"started_at"`
	FinishedAt  string          `json:"finished_at,omitempty"`
}

// executionResultSchema is the JSON schema codex MUST return as its final
// message, in every stage. It distinguishes completion from a durable wait,
// human input, failure, and "nobody needs to act" instead of inferring
// completion from the process exit code, and it always carries the approval
// verdict: whether a side effect needs review is the model's judgment about what
// it is about to do, not a property of the Task's declared action_type.
//
// outcome=observing exists because execution may discover after investigating
// that the matter is real but asks nothing of anyone. Forcing that into completed
// (nothing was done) or failed (nothing went wrong) destroys the distinction.
const executionResultSchema = `{
  "type":"object",
  "additionalProperties":false,
  "required":["needs_approval","outcome","summary","user_message","progress_summary","failure_reason","needs_followup","enrichments","effects","proposal","waiting"],
  "properties":{
    "needs_approval":{"type":"boolean","description":"Approval verdict for the next controlled side effect; criteria are defined by APPROVAL_POLICY."},
    "outcome":{"type":"string","enum":["completed","observing","waiting","needs_human","failed"]},
    "summary":{"type":"string","minLength":1},
    "user_message":{"type":"string","description":"The concise natural-language result for the user at the Task's source message. Jarvis replaces its existing processing reply with this text. Exclude internal reasoning, tool steps, message IDs, self-check notes and audit detail. Leave empty only when the Task has no source conversation or no source-facing message is appropriate."},
    "progress_summary":{"type":"string","maxLength":1000,"description":"Where this whole matter now stands, in a few sentences, written for someone reading it cold weeks later: what is settled, what is still open, what happens next. This spans all runs of the Task, unlike summary which covers only this run. Rewrite it in full each time, within 1000 characters: when you run out of room, compact finished detail into one conclusion rather than dropping the tail. Leave it an empty string only when this run changed nothing about where the matter stands."},
    "failure_reason":{"type":"string"},
    "needs_followup":{"type":"string"},
    "enrichments":{
      "type":"array",
      "items":{
        "type":"object",
        "additionalProperties":false,
        "required":["kind","label","content"],
        "properties":{
          "kind":{"type":"string","minLength":1},
          "label":{"type":"string","minLength":1},
          "content":{"type":"string","minLength":1}
        }
      }
    },
    "effects":{
      "type":"array",
      "description":"Real-world side effects you actually produced (message sent, doc created, meeting scheduled, MR opened, permission requested, ...). Declare one entry per external write. kind is a free-form label you may invent. All item fields are required by Structured Outputs; unused title/url/target/preview/extra must be empty strings. Put extra metadata in extra as JSON text. Display-only, not verified.",
      "items":{
        "type":"object",
        "additionalProperties":false,
        "required":["kind","title","url","target","preview","extra"],
        "properties":{
          "kind":{"type":"string","minLength":1},
          "title":{"type":"string"},
          "url":{"type":"string"},
          "target":{"type":"string"},
          "preview":{"type":"string"},
          "extra":{"type":"string","description":"Free-form metadata as JSON text (e.g. {\"message_id\":\"om_…\",\"chat_name\":\"…\"}); use empty string when none. Do not invent top-level fields."}
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

// appendSchemaContract states the final-message contract inside the prompt. It
// is used when the CLI cannot enforce it with --output-schema (traecli on
// `exec resume`), so the model still knows the exact shape it must return.
func appendSchemaContract(prompt, schemaDef string) string {
	return prompt + `

BEGIN_FINAL_MESSAGE_CONTRACT（本轮不能由 CLI 强制返回格式，必须你自己遵守）
你的最后一条消息必须是且只是一个 JSON 对象，且严格符合下面的 JSON Schema：
不要包裹 markdown 代码围栏，不要在 JSON 前后添加任何解释文字，不要输出多个 JSON 值，
不要出现 Schema 之外的字段。
` + schemaDef + `
END_FINAL_MESSAGE_CONTRACT`
}

// schemaRewritePrompt hands a contract violation back to the same session so it
// re-emits a valid final message. The work of the previous turn already
// happened (and may have produced real side effects), so it must not be redone.
func schemaRewritePrompt(err error) string {
	return `你上一条最终消息不符合要求的返回格式，校验报错如下：

` + err.Error() + `

不要重做上一轮已经完成的工作，也不要重复任何已经产生的外部写入。
只需按下面的格式，把上一轮的真实结果重新输出一次最终消息。`
}

type executionPromptPayload struct {
	PromptVersion string        `json:"prompt_version"`
	Task          executionTask `json:"task"`
	// ExecutionContext is Task.background verbatim: the snapshot M3 froze when
	// the Todo was admitted. It rides along whole so M5 reads the same world the
	// clue was judged against.
	ExecutionContext json.RawMessage `json:"execution_context"`
	// CurrentWorld is loaded when this run starts, not frozen with the clue. It
	// answers what ExecutionContext structurally cannot: what else Jarvis is
	// already working on, and what it just finished.
	CurrentWorld         *currentWorld         `json:"current_world,omitempty"`
	RepoPath             string                `json:"repo_path,omitempty"`
	ExecutionSupplements []ExecutionSupplement `json:"execution_supplements,omitempty"`
	PreviousRuns         []priorRunSummary     `json:"previous_runs,omitempty"`
}

type executionTask struct {
	ID             uint64  `json:"id"`
	TitleHint      string  `json:"title_hint"`
	TargetHint     string  `json:"target_hint"`
	CurrentStatus  string  `json:"current_status"`
	CurrentSummary *string `json:"current_summary,omitempty"`
	LastProgressAt string  `json:"last_progress_at,omitempty"`
	// ProjectID is the Task's own binding. The snapshot usually carries the
	// project too, but a Task can be bound without one.
	ProjectID *uint64 `json:"project_id,omitempty"`
	// SourcePayload is the source-owned semantic input forwarded verbatim for
	// every Task source. M5 treats it as evidence, not an execution contract.
	SourcePayload json.RawMessage `json:"source_payload"`
}

// buildTaskContext assembles the shared TASK_CONTEXT block. M3 output is a clue,
// not a confirmed contract; M5 owns the actual goal, scope, action selection,
// and execution. Source semantics and the frozen background both ride through
// verbatim. Validation is fail-fast.
func buildTaskContext(task *domain.Task, repoPath string, previousRuns []priorRunSummary, world *currentWorld) ([]ExecutionSupplement, []byte, error) {
	if task == nil || task.ID == 0 {
		return nil, nil, fmt.Errorf("execution prompt Task is invalid")
	}
	if strings.TrimSpace(task.Title) == "" || strings.TrimSpace(task.ActionType) == "" {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d missing title or action_type", task.ID)
	}
	if len(bytes.TrimSpace(task.SourcePayload)) == 0 {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d missing source_payload", task.ID)
	}
	background, err := requireBackgroundSnapshot(task)
	if err != nil {
		return nil, nil, err
	}
	supplements, err := decodeExecutionSupplements(task.ExecutionSupplements)
	if err != nil {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d execution_supplements invalid: %w", task.ID, err)
	}
	promptTask := executionTask{
		ID: task.ID, TitleHint: task.Title, TargetHint: task.Target,
		CurrentStatus: task.Status, CurrentSummary: task.Summary,
		ProjectID:     task.ProjectID,
		SourcePayload: rawJSON(task.SourcePayload),
	}
	if task.LastProgressAt != nil {
		promptTask.LastProgressAt = task.LastProgressAt.UTC().Format(time.RFC3339)
	}
	payload := executionPromptPayload{
		PromptVersion:        ExecutionPromptVersion,
		ExecutionContext:     background,
		CurrentWorld:         world,
		RepoPath:             repoPath,
		ExecutionSupplements: supplements,
		PreviousRuns:         previousRuns,
		Task:                 promptTask,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("encode execution prompt payload task_id=%d: %w", task.ID, err)
	}
	return supplements, encoded, nil
}

// requireBackgroundSnapshot returns Task.background verbatim. The context is
// assembled once in M3 and frozen; reshaping it here would hand M5 a different
// world than the one the Todo was admitted against. Decoding is validation only.
func requireBackgroundSnapshot(task *domain.Task) (json.RawMessage, error) {
	if _, err := contextsnap.Decode(task.Background); err != nil {
		return nil, fmt.Errorf("execution prompt Task id=%d background invalid: %w", task.ID, err)
	}
	return rawJSON(task.Background), nil
}

// renderPrompt glues the stage instructions, the shared-memory block, the
// supplement directive block, and the encoded TASK_CONTEXT into the final codex
// prompt. sharedMemory (可信共享记忆) is injected right after the instructions and
// before TASK_CONTEXT（不可信业务数据），即受信任指令区；为空则不注入。
func renderPrompt(instructions, toolCatalog, sharedMemory, skills string, supplements []ExecutionSupplement, encoded []byte) string {
	directive := formatExecutionSupplementDirective(supplements)
	prompt := strings.TrimSpace(instructions)
	if block := sharedmem.RenderBlock(sharedMemory); block != "" {
		prompt += "\n\n" + block
	}
	if block := strings.TrimSpace(skills); block != "" {
		prompt += "\n\n" + block
	}
	if block := strings.TrimSpace(toolCatalog); block != "" {
		prompt += "\n\n" + block
	}
	return prompt + directive +
		"\n\nTASK_CONTEXT_LENGTH_BYTES=" + fmt.Sprintf("%d", len(encoded)) +
		"\nBEGIN_TASK_CONTEXT\n" + string(encoded) + "\nEND_TASK_CONTEXT"
}

// executionPromptInput carries everything assembled for one run. It is a struct
// rather than a parameter list because the two builders need the same nine-plus
// values and positional arguments stopped being readable.
type executionPromptInput struct {
	SystemPrompt   string
	ApprovalPolicy string
	Task           *domain.Task
	RepoPath       string
	ToolCatalog    string
	SharedMemory   string
	WorkRules      string
	Skills         string
	// PreviousRuns carry prior attempt results for this same Task.
	PreviousRuns []priorRunSummary
	// CurrentWorld is the live Task/Todo slice read when the run starts.
	CurrentWorld *currentWorld
}

// buildExecutionPrompt assembles the prompt for a Task's first pass. Every
// action_type takes this one path: codex investigates, decides the real goal and
// action, and then judges against the editable approvalPolicy whether the side
// effect it is about to cause needs human review — code changes included. It
// gives codex the complete M3 clue, the whole frozen background, the live world
// slice, and the resolved repo.
// task.execution_supplements (M5-only) are injected as high-priority directives.
func buildExecutionPrompt(in executionPromptInput) (string, error) {
	renderedSystemPrompt, err := prompttemplate.Render(prompttemplate.StageM5, in.SystemPrompt, in.WorkRules, in.ApprovalPolicy)
	if err != nil {
		return "", fmt.Errorf("render M5 execution system prompt: %w", err)
	}
	supplements, encoded, err := buildTaskContext(in.Task, in.RepoPath, in.PreviousRuns, in.CurrentWorld)
	if err != nil {
		return "", err
	}

	instructions := renderedSystemPrompt + "\n\n" + m5PhaseExecute
	instructions += repoInstruction(in.RepoPath)

	return renderPrompt(instructions, in.ToolCatalog, in.SharedMemory, in.Skills, supplements, encoded), nil
}

// repoInstruction only tells codex where the resolved working copy is. Delivery
// behavior belongs to the concrete Task and work rules, not this generic prompt.
func repoInstruction(repoPath string) string {
	if strings.TrimSpace(repoPath) == "" {
		return ""
	}
	return "\n\n当前工作目录已切到 repo：" + repoPath + "。"
}

// buildApplyPrompt assembles the apply-stage prompt after a human approved a
// proposal. The approved action + full artifact is embedded verbatim and codex is
// told to land it faithfully for real. Its final message must satisfy
// executionResultSchema.
func buildApplyPrompt(in executionPromptInput, proposal *codexProposal) (string, error) {
	if proposal == nil {
		return "", fmt.Errorf("apply prompt Task id=%d has no approved proposal", in.Task.ID)
	}
	renderedSystemPrompt, err := prompttemplate.Render(prompttemplate.StageM5, in.SystemPrompt, in.WorkRules, in.ApprovalPolicy)
	if err != nil {
		return "", fmt.Errorf("render M5 apply system prompt: %w", err)
	}
	supplements, encoded, err := buildTaskContext(in.Task, in.RepoPath, in.PreviousRuns, in.CurrentWorld)
	if err != nil {
		return "", err
	}
	approved, err := json.Marshal(map[string]string{
		"action":   proposal.Action,
		"target":   proposal.Target,
		"artifact": proposal.Artifact,
	})
	if err != nil {
		return "", fmt.Errorf("encode approved proposal task_id=%d: %w", in.Task.ID, err)
	}

	instructions := renderedSystemPrompt + "\n\n" + m5PhaseApply + `

APPROVED_PROPOSAL=` + string(approved)
	instructions += repoInstruction(in.RepoPath)

	return renderPrompt(instructions, in.ToolCatalog, in.SharedMemory, in.Skills, supplements, encoded), nil
}
