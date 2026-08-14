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
const ExecutionPromptVersion = "task-exec-v11-task-state-evidence"

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
  "required":["needs_approval","outcome","summary","progress_summary","failure_reason","needs_followup","enrichments","effects","proposal","waiting"],
  "properties":{
    "needs_approval":{"type":"boolean","description":"Approval verdict for the next controlled side effect; criteria are defined by APPROVAL_POLICY."},
    "outcome":{"type":"string","enum":["completed","observing","waiting","needs_human","failed"]},
    "summary":{"type":"string","minLength":1},
    "progress_summary":{"type":"string","description":"Where this whole matter now stands, in a few sentences, written for someone reading it cold weeks later: what is settled, what is still open, what happens next. This spans all runs of the Task, unlike summary which covers only this run. Rewrite it in full each time. Leave it an empty string only when this run changed nothing about where the matter stands."},
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
	PromptVersion        string                `json:"prompt_version"`
	Task                 executionTask         `json:"task"`
	ExecutionContext     executionContext      `json:"execution_context"`
	BackgroundLookup     string                `json:"background_lookup"`
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
	// SourcePayload is the source-owned semantic input forwarded verbatim for
	// every Task source. M5 treats it as evidence, not an execution contract.
	SourcePayload json.RawMessage `json:"source_payload"`
}

// executionContext is the small part of the frozen background M5 needs before
// it starts investigating. The complete immutable Task.background stays in the
// database and is available through BackgroundLookup when a Task actually needs
// more of its creation-time world.
type executionContext struct {
	Principal      *executionPrincipal   `json:"principal,omitempty"`
	Project        *executionProject     `json:"project,omitempty"`
	Group          *executionGroup       `json:"group,omitempty"`
	Assigner       *executionAssigner    `json:"assigner,omitempty"`
	SourceMessages []contextsnap.Message `json:"source_messages,omitempty"`
}

type executionPrincipal struct {
	OpenID string `json:"open_id,omitempty"`
	Name   string `json:"name,omitempty"`
}

type executionProject struct {
	ID     uint64  `json:"id"`
	Code   *string `json:"code,omitempty"`
	Name   string  `json:"name,omitempty"`
	Role   string  `json:"role,omitempty"`
	Status string  `json:"status,omitempty"`
}

type executionGroup struct {
	ID        uint64  `json:"id"`
	ChatID    string  `json:"chat_id,omitempty"`
	Name      *string `json:"name,omitempty"`
	ProjectID *uint64 `json:"project_id,omitempty"`
}

type executionAssigner struct {
	OpenID   string  `json:"open_id"`
	Name     *string `json:"name,omitempty"`
	Role     *string `json:"role,omitempty"`
	Relation *string `json:"relation,omitempty"`
}

// buildTaskContext assembles the shared TASK_CONTEXT block. M3 output is a clue,
// not a confirmed contract; M5 owns the actual goal, scope, action selection,
// and execution. Source semantics ride through verbatim; the frozen background
// is projected to the small execution context above and remains queryable in
// full. Validation is fail-fast.
func buildTaskContext(task *domain.Task, repoPath string, previousRuns []priorRunSummary) ([]ExecutionSupplement, []byte, error) {
	if task == nil || task.ID == 0 {
		return nil, nil, fmt.Errorf("execution prompt Task is invalid")
	}
	if strings.TrimSpace(task.Title) == "" || strings.TrimSpace(task.ActionType) == "" {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d missing title or action_type", task.ID)
	}
	if len(bytes.TrimSpace(task.SourcePayload)) == 0 {
		return nil, nil, fmt.Errorf("execution prompt Task id=%d missing source_payload", task.ID)
	}
	context, err := projectExecutionContext(task)
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
		SourcePayload: rawJSON(task.SourcePayload),
	}
	if task.LastProgressAt != nil {
		promptTask.LastProgressAt = task.LastProgressAt.UTC().Format(time.RFC3339)
	}
	payload := executionPromptPayload{
		PromptVersion:        ExecutionPromptVersion,
		ExecutionContext:     context,
		BackgroundLookup:     fmt.Sprintf("jarvis-tools get-task --id %d", task.ID),
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

func projectExecutionContext(task *domain.Task) (executionContext, error) {
	snapshot, err := contextsnap.Decode(task.Background)
	if err != nil {
		if task.SourceType == "todo" {
			return executionContext{}, nil
		}
		return executionContext{}, fmt.Errorf("execution prompt Task id=%d background invalid: %w", task.ID, err)
	}
	result := executionContext{}
	if snapshot.Principal != nil {
		result.Principal = &executionPrincipal{
			OpenID: snapshot.Principal.OpenID,
			Name:   snapshot.Principal.Name,
		}
	}
	if snapshot.Project != nil {
		result.Project = &executionProject{
			ID: snapshot.Project.ID, Code: snapshot.Project.Code, Name: snapshot.Project.Name,
			Role: snapshot.Project.Role, Status: snapshot.Project.Status,
		}
	} else if task.ProjectID != nil {
		result.Project = &executionProject{ID: *task.ProjectID}
	}
	if snapshot.Group != nil {
		result.Group = &executionGroup{
			ID: snapshot.Group.ID, ChatID: snapshot.Group.ChatID,
			Name: snapshot.Group.Name, ProjectID: snapshot.Group.ProjectID,
		}
	}
	if snapshot.Assigner != nil {
		result.Assigner = &executionAssigner{
			OpenID: snapshot.Assigner.OpenID, Name: snapshot.Assigner.Name,
			Role: snapshot.Assigner.Role, Relation: snapshot.Assigner.Relation,
		}
	}
	seen := make(map[string]struct{}, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		id := strings.TrimSpace(message.MessageID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result.SourceMessages = append(result.SourceMessages, message)
	}
	return result, nil
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

// buildExecutionPrompt assembles the prompt for a Task's first pass. Every
// action_type takes this one path: codex investigates, decides the real goal and
// action, and then judges against the editable approvalPolicy whether the side
// effect it is about to cause needs human review — code changes included. It
// gives codex the complete M3 clue, a small projection of frozen context, and
// the resolved repo. Complete Task.background is loaded only when needed.
// task.execution_supplements (M5-only) are injected as high-priority directives.
// previousRuns (if any) carry prior attempt results.
func buildExecutionPrompt(systemPrompt, approvalPolicy string, task *domain.Task, repoPath, toolCatalog, sharedMemory, workRules, skills string, previousRuns []priorRunSummary) (string, error) {
	renderedSystemPrompt, err := prompttemplate.Render(prompttemplate.StageM5, systemPrompt, workRules, approvalPolicy)
	if err != nil {
		return "", fmt.Errorf("render M5 execution system prompt: %w", err)
	}
	supplements, encoded, err := buildTaskContext(task, repoPath, previousRuns)
	if err != nil {
		return "", err
	}

	instructions := renderedSystemPrompt + "\n\n" + m5PhaseExecute
	instructions += repoInstruction(repoPath)

	return renderPrompt(instructions, toolCatalog, sharedMemory, skills, supplements, encoded), nil
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
func buildApplyPrompt(systemPrompt, approvalPolicy string, task *domain.Task, proposal *codexProposal, repoPath, toolCatalog, sharedMemory, workRules, skills string, previousRuns []priorRunSummary) (string, error) {
	if proposal == nil {
		return "", fmt.Errorf("apply prompt Task id=%d has no approved proposal", task.ID)
	}
	renderedSystemPrompt, err := prompttemplate.Render(prompttemplate.StageM5, systemPrompt, workRules, approvalPolicy)
	if err != nil {
		return "", fmt.Errorf("render M5 apply system prompt: %w", err)
	}
	supplements, encoded, err := buildTaskContext(task, repoPath, previousRuns)
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

	instructions := renderedSystemPrompt + "\n\n" + m5PhaseApply + `

APPROVED_PROPOSAL=` + string(approved)
	instructions += repoInstruction(repoPath)

	return renderPrompt(instructions, toolCatalog, sharedMemory, skills, supplements, encoded), nil
}
