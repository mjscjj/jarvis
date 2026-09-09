package execute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/contextpack"
	"jarvis/internal/domain"
	"jarvis/internal/prompttemplate"
	"jarvis/internal/sharedmem"
)

// ExecutionPromptVersion identifies the prompt contract for auditing.
const ExecutionPromptVersion = "task-exec-v16-capture"

const (
	m5PhaseExecute = `BEGIN_M5_PHASE
phase=execute
END_M5_PHASE`
	m5PhaseResumeWaiting = `BEGIN_M5_PHASE
phase=resume_waiting
END_M5_PHASE`
	m5PhaseResumeHuman = `BEGIN_M5_PHASE
phase=resume_human
END_M5_PHASE`
)

// executionResultSchema is the JSON schema codex MUST return as its final
// message, in every stage. It distinguishes completion from a durable wait,
// human input, failure, and "nobody needs to act" instead of inferring
// completion from the process exit code.
//
// Asking the principal anything — including asking permission for a side effect
// the APPROVAL_POLICY gates — is outcome=needs_human plus a question. There is
// no separate approval verdict or approval stage: the answer resumes this exact
// Codex session, so the agent decides what to do with it in context rather than
// replaying a frozen artifact.
//
// outcome=observing exists because execution may discover after investigating
// that the matter is real but asks nothing of anyone. Forcing that into completed
// (nothing was done) or failed (nothing went wrong) destroys the distinction.
const executionResultSchema = `{
  "type":"object",
  "additionalProperties":false,
  "required":["outcome","summary","progress_summary","failure_reason","question","enrichments","effects","waiting"],
  "properties":{
    "outcome":{"type":"string","enum":["completed","observing","waiting","needs_human","failed"]},
    "summary":{"type":"string","minLength":1},
    "progress_summary":{"type":"string","maxLength":1000,"description":"Where this whole matter now stands, in a few sentences, written for someone reading it cold weeks later: what is settled, what is still open, what happens next. This spans all runs of the Task, unlike summary which covers only this run. Rewrite it in full each time, within 1000 characters: when you run out of room, compact finished detail into one conclusion rather than dropping the tail. Leave it an empty string only when this run changed nothing about where the matter stands."},
    "failure_reason":{"type":"string"},
    "question":{
      "type":["object","null"],
      "additionalProperties":false,
      "description":"Required with outcome=needs_human, null otherwise. The compact decision card the principal sees. Include the minimum context needed to decide, not investigation logs or repeated history; the answer comes back to this same session as JSON of the field names plus clicked.",
      "required":["title","body","fields"],
      "properties":{
        "title":{"type":"string","minLength":1,"description":"One short sentence stating exactly what the principal must decide."},
        "body":{"type":"string","description":"Markdown. Start with 1-3 concise sentences covering what this is, why a decision is needed now, and the main impact. Omit investigation logs, tool calls, repeated history, and unrelated detail. For a content-changing side effect, also include the complete exact text you would write or send; never shorten that text for compactness."},
        "fields":{
          "type":"array",
          "minItems":1,
          "description":"At least one button is required; without it the principal cannot answer. For a simple binary decision, prefer exactly two buttons with short labels.",
          "items":{
            "type":"object",
            "additionalProperties":false,
            "required":["type","name","label","options","url","style"],
            "properties":{
              "type":{"type":"string","enum":["button","select","multi_select","input","link"]},
              "name":{"type":"string","description":"Key this field answers under. Empty only for link."},
              "label":{"type":"string","minLength":1},
              "options":{"type":"array","items":{"type":"string"},"description":"Choices for select/multi_select; empty array otherwise."},
              "url":{"type":"string","description":"Target for link; empty string otherwise."},
              "style":{"type":"string","description":"Button emphasis: primary, danger, or empty."}
            }
          }
        }
      }
    },
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
	PromptVersion        string                `json:"prompt_version"`
	Task                 executionTask         `json:"task"`
	Context              json.RawMessage       `json:"context"`
	RepoPath             string                `json:"repo_path,omitempty"`
	ExecutionSupplements []ExecutionSupplement `json:"execution_supplements,omitempty"`
	History              *runHistory           `json:"history,omitempty"`
}

type executionTask struct {
	TodoID         *uint64 `json:"todo_id,omitempty"`
	SourceType     string  `json:"source_type"`
	SourceID       *uint64 `json:"source_id,omitempty"`
	ID             uint64  `json:"id"`
	TitleHint      string  `json:"title_hint"`
	TargetHint     string  `json:"target_hint"`
	CurrentStatus  string  `json:"current_status"`
	CurrentSummary *string `json:"current_summary,omitempty"`
	LastProgressAt string  `json:"last_progress_at,omitempty"`
	// ProjectID is the Task's own binding. The snapshot usually carries the
	// project too, but a Task can be bound without one.
	ProjectID *uint64 `json:"project_id,omitempty"`
}

// buildTaskContext assembles the shared TASK_CONTEXT block. M3 output is a clue,
// not a confirmed contract; M5 owns the actual goal, scope, action selection,
// and execution. The overview projects direct evidence and reading entrypoints
// from the frozen source payload. Validation is fail-fast.
func buildTaskContext(task *domain.Task, repoPath string, history *runHistory) ([]ExecutionSupplement, []byte, error) {
	if task == nil || task.ID == 0 {
		return nil, nil, fmt.Errorf("execution prompt Task is invalid")
	}
	if strings.TrimSpace(task.Title) == "" || strings.TrimSpace(task.ActionType) == "" {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d missing title or action_type", task.ID)
	}
	if len(bytes.TrimSpace(task.SourcePayload)) == 0 {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d missing source_payload", task.ID)
	}
	overview, err := contextpack.Read(task.SourcePayload, "", "")
	if err != nil {
		return nil, nil, err
	}
	supplements, err := decodeExecutionSupplements(task.ExecutionSupplements)
	if err != nil {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d execution_supplements invalid: %w", task.ID, err)
	}
	promptTask := executionTask{
		ID: task.ID, TodoID: task.TodoID, SourceType: task.SourceType, SourceID: task.SourceID, TitleHint: task.Title, TargetHint: task.Target,
		CurrentStatus: task.Status, CurrentSummary: task.Summary,
		ProjectID: task.ProjectID,
	}
	if task.LastProgressAt != nil {
		promptTask.LastProgressAt = task.LastProgressAt.UTC().Format(time.RFC3339)
	}
	payload := executionPromptPayload{
		PromptVersion:        ExecutionPromptVersion,
		Context:              overview,
		RepoPath:             repoPath,
		ExecutionSupplements: supplements,
		History:              history,
		Task:                 promptTask,
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
	History        *runHistory
}

// buildExecutionPrompt assembles the prompt for a Task's first pass. Every
// action_type takes this one path: codex investigates, decides the real goal and
// action, and then judges against the editable approvalPolicy whether the side
// effect it is about to cause needs human review — code changes included. It
// gives codex direct evidence, scene and background reading entrypoints, current
// Task state, history availability and the resolved repo.
// task.execution_supplements (M5-only) are injected as high-priority directives.
func buildExecutionPrompt(in executionPromptInput) (string, error) {
	renderedSystemPrompt, err := prompttemplate.Render(prompttemplate.StageM5, in.SystemPrompt, in.WorkRules, in.ApprovalPolicy)
	if err != nil {
		return "", fmt.Errorf("render M5 execution system prompt: %w", err)
	}
	supplements, encoded, err := buildTaskContext(in.Task, in.RepoPath, in.History)
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
