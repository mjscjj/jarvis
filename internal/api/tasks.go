package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/contextpack"
	"jarvis/internal/effectops"
	"jarvis/internal/execute"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type finishTaskRequest struct {
	ExpectedVersion *int32          `json:"expected_version"`
	Status          string          `json:"status"`
	Result          json.RawMessage `json:"result"`
}

type closeTaskRequest struct {
	ExpectedVersion *int32          `json:"expected_version"`
	Result          json.RawMessage `json:"result"`
	ActorType       string          `json:"actor_type"`
}

type updateTaskRequest struct {
	ExpectedVersion *int32  `json:"expected_version"`
	Title           *string `json:"title"`
	Target          *string `json:"target"`
	Summary         *string `json:"summary"`
	Instruction     *string `json:"instruction"`
	Reason          string  `json:"reason"`
	ActorType       string  `json:"actor_type"`
}

func ListTasks(service execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		page, err := positiveQueryInt(c.Query("page"), 1, "page")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		pageSize, err := positiveQueryInt(c.Query("page_size"), 20, "page_size")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		statuses, err := execute.ParseStatuses(c.Query("status"))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		if len(statuses) == 0 {
			statuses = []string{"pending"}
		}
		filter := execute.TaskFilter{
			Statuses: statuses, Page: page, PageSize: pageSize,
			Query: c.Query("query"), SourceMessageID: c.Query("source_message_id"),
			Plugin:            strings.TrimSpace(c.Query("plugin")),
			ActionType:        strings.TrimSpace(c.Query("action_type")),
			ExcludeActionType: strings.TrimSpace(c.Query("exclude_action_type")),
		}
		if raw := strings.TrimSpace(c.Query("project_id")); raw != "" {
			value, err := strconv.ParseUint(raw, 10, 64)
			if err != nil || value == 0 {
				writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("project_id must be a positive integer"))
				return
			}
			filter.ProjectID = &value
		}
		if raw := strings.TrimSpace(c.Query("group_id")); raw != "" {
			value, err := strconv.ParseUint(raw, 10, 64)
			if err != nil || value == 0 {
				writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("group_id must be a positive integer"))
				return
			}
			filter.GroupID = &value
		}
		if raw := strings.TrimSpace(c.Query("from")); raw != "" {
			from, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("from must be RFC3339: %w", err))
				return
			}
			filter.From = &from
		}
		if raw := strings.TrimSpace(c.Query("until")); raw != "" {
			until, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("until must be RFC3339: %w", err))
				return
			}
			filter.Until = &until
		}
		result, err := service.ListTasks(ctx, filter)
		if err != nil {
			if errors.Is(err, execute.ErrInvalidInput) {
				writeAPIError(c, consts.StatusBadRequest, 40020, err)
			} else {
				writeAPIError(c, consts.StatusInternalServerError, 50020, err)
			}
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// GetTask returns one Task's detail view.
func GetTask(service execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		result, err := service.GetTask(ctx, taskID)
		if err != nil {
			switch {
			case errors.Is(err, execute.ErrInvalidInput):
				writeAPIError(c, consts.StatusBadRequest, 40020, err)
			case errors.Is(err, execute.ErrTaskNotFound):
				writeAPIError(c, consts.StatusNotFound, 40420, err)
			default:
				writeAPIError(c, consts.StatusInternalServerError, 50020, err)
			}
			return
		}
		section, messageID := c.Query("context"), c.Query("message_id")
		view, err := contextpack.ReadFor(result.SourcePayload, result.SourceType, section, messageID)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40020, err)
			return
		}
		view, err = contextRange(c, view)
		if err != nil {
			writeAPIError(c, 400, 40020, err)
			return
		}
		if c.Query("offset") != "" || c.Query("length") != "" || messageID != "" || (section != "" && section != "full" && section != "overview") {
			c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": result.ID, "version": result.Version, "context": view}})
			return
		}
		result.SourcePayload = view
		if section != "full" {
			result.ExecutionResult = nil
			result.Target = ""
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// ListTaskRuns returns a Task's execution audit history (ExecutionRun list),
// newest first, powering the task detail drawer's execution timeline.
func ListTaskRuns(service execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40025, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		page, err := positiveQueryInt(c.Query("page"), 1, "page")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40025, err)
			return
		}
		size, err := positiveQueryInt(c.Query("page_size"), 20, "page_size")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40025, err)
			return
		}
		result, err := service.ListRuns(ctx, taskID, execute.RunFilter{Page: page, PageSize: size})
		if err != nil {
			if errors.Is(err, execute.ErrInvalidInput) {
				writeAPIError(c, consts.StatusBadRequest, 40025, err)
			} else {
				writeAPIError(c, consts.StatusInternalServerError, 50025, err)
			}
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// GetTaskRunOutput returns the latest Codex invocation's complete prompt,
// stdout JSONL stream and stderr. The files are written while Codex is running,
// so polling this endpoint provides a simple live view without a second event
// transport.
func GetTaskRunOutput(service execute.TaskRunOutputReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40026, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		result, err := service.LatestTaskRunOutput(ctx, taskID)
		if err != nil {
			switch {
			case errors.Is(err, execute.ErrInvalidInput):
				writeAPIError(c, consts.StatusBadRequest, 40026, err)
			case errors.Is(err, execute.ErrTaskNotFound):
				writeAPIError(c, consts.StatusNotFound, 40426, err)
			default:
				writeAPIError(c, consts.StatusInternalServerError, 50026, err)
			}
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func FinishTask(service execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40021, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		var request finishTaskRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, fmt.Errorf("expected_version is required"))
			return
		}
		status := strings.TrimSpace(request.Status)
		// FinishTask is the manual button path (人工「手动完成」/「失败」). Tag the
		// stored result with a manual stage so the UI can tell a human-marked
		// failure apart from a real codex execution failure (stage=executed).
		tagged, err := tagResultStage(request.Result, manualStage(status))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		result, err := service.Finish(ctx, execute.FinishInput{
			TaskID: taskID, ExpectedVersion: *request.ExpectedVersion,
			Status: status, Result: tagged, ActorType: "user",
		})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// CloseTask resolves an existing non-terminal Task with explicit evidence.
// The caller's stage is audit context, not an authorization identity.
func CloseTask(service execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40032, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		var request closeTaskRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40032, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40032, fmt.Errorf("expected_version is required"))
			return
		}
		actorType := strings.TrimSpace(request.ActorType)
		if actorType == "" {
			writeAPIError(c, consts.StatusBadRequest, 40032, fmt.Errorf("actor_type is required"))
			return
		}
		result, err := service.Close(ctx, execute.CloseInput{
			TaskID: taskID, ExpectedVersion: *request.ExpectedVersion,
			Result: request.Result, ActorType: actorType,
		})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// UpdateTask lets a trusted Agent revise the mutable current Task surface
// without rewriting frozen source evidence or pretending the goal is complete.
func UpdateTask(service execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40033, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		var request updateTaskRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, fmt.Errorf("expected_version is required"))
			return
		}
		actorType := strings.TrimSpace(request.ActorType)
		if actorType == "" {
			writeAPIError(c, consts.StatusBadRequest, 40033, fmt.Errorf("actor_type is required"))
			return
		}
		result, err := service.UpdateTask(ctx, execute.TaskUpdateInput{
			TaskID: taskID, ExpectedVersion: *request.ExpectedVersion,
			Title: request.Title, Target: request.Target, Summary: request.Summary,
			Instruction: request.Instruction, Reason: request.Reason, ActorType: actorType,
		})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// ExecuteTask triggers agent-driven execution of a Task. The agent investigates
// and either finishes the work or, when it needs the principal to decide or to
// permit a gated side effect, parks the Task at needs_human behind a question
// card. The click itself does not authorize external writes.
func ExecuteTask(executor *execute.AgentExecutor) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40023, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		result, err := executor.KickExecute(ctx, execute.ExecuteInput{TaskID: taskID})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

type interruptTaskRequest struct {
	ExpectedVersion *int32 `json:"expected_version"`
}

// InterruptTask cancels the live Codex process for an executing Task. The
// executor waits for the regular run audit and Task failure result to be saved
// before this handler returns.
func InterruptTask(executor *execute.AgentExecutor) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40031, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		var request interruptTaskRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40031, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40031, fmt.Errorf("expected_version is required"))
			return
		}
		result, err := executor.Interrupt(ctx, taskID, *request.ExpectedVersion)
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// RerunTask re-executes a finished (done/failed) Task. Persisted
// execution_supplements are included automatically on every run.
func RerunTask(executor *execute.AgentExecutor) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40023, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		result, err := executor.KickRerun(ctx, taskID)
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

type resumeTaskRequest struct {
	ExpectedVersion *int32 `json:"expected_version"`
	Response        string `json:"response"`
}

// ResumeTaskAfterHuman answers the question the Codex session stopped on, from
// the backend rather than the Feishu card. It is deliberately distinct from
// rerun: the session continues where it paused instead of starting over.
func ResumeTaskAfterHuman(executor *execute.AgentExecutor) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40030, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		var request resumeTaskRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40030, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40030, fmt.Errorf("expected_version is required"))
			return
		}
		result, err := executor.KickResumeAfterHuman(ctx, taskID, *request.ExpectedVersion, request.Response, "backend")
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

type supplementTaskRequest struct {
	ExpectedVersion *int32 `json:"expected_version"`
	Note            string `json:"note"`
}

// SupplementTask appends a human clarification/instruction to a Task's M5-only
// execution_supplements. It does not trigger execution.
func SupplementTask(service execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40026, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		var request supplementTaskRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40026, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40026, fmt.Errorf("expected_version is required"))
			return
		}
		result, err := service.Supplement(ctx, execute.SupplementInput{
			TaskID: taskID, ExpectedVersion: *request.ExpectedVersion, Note: request.Note, Channel: "backend",
		})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

type recallEffectMessageRequest struct {
	MessageID string `json:"message_id"`
}

// RecallEffectMessage recalls one Feishu message this Task declared in its
// effects and marks that effect as recalled. The click itself is the human
// confirmation for a high-risk, irreversible external write, so no
// expected_version is required; the reloaded Task (with a bumped version) is
// returned so the caller can refresh the drawer it was clicked from.
func RecallEffectMessage(recaller *effectops.MessageRecaller, tasks execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40031, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		var request recallEffectMessageRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40031, err)
			return
		}
		err = recaller.Recall(ctx, taskID, request.MessageID)
		if err != nil {
			writeEffectOperationError(c, err)
			return
		}
		result, err := tasks.GetTask(ctx, taskID)
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func writeEffectOperationError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, effectops.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40022, err)
	case errors.Is(err, effectops.ErrTaskNotFound):
		writeAPIError(c, consts.StatusNotFound, 40420, err)
	case errors.Is(err, effectops.ErrRecallTargetNotFound):
		writeAPIError(c, consts.StatusNotFound, 40421, err)
	case errors.Is(err, effectops.ErrMessageAlreadyRecalled), errors.Is(err, effectops.ErrVersionConflict):
		writeAPIError(c, consts.StatusConflict, 40921, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50021, fmt.Errorf("effect operation failed: %s", strings.TrimSpace(err.Error())))
	}
}

// manualStage maps a manual finish status to the execution_result stage tag so
// the UI distinguishes a human-marked failure from a codex execution failure.
func manualStage(status string) string {
	switch status {
	case "failed":
		return "manual_failed"
	case "observing":
		return "manual_observing"
	}
	return "manual_done"
}

// tagResultStage injects a "stage" field into the manual finish result JSON so
// the frontend can classify the outcome. It fails-fast on malformed JSON (the
// store would reject it anyway) but preserves every field the caller sent; an
// explicit caller-provided stage is not overwritten.
func tagResultStage(raw json.RawMessage, stage string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("result is required")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("result must be a JSON object: %w", err)
	}
	if _, ok := obj["stage"]; !ok {
		obj["stage"] = stage
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode tagged result: %w", err)
	}
	return encoded, nil
}

func writeExecutionError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, execute.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40022, err)
	case errors.Is(err, execute.ErrTaskNotFound):
		writeAPIError(c, consts.StatusNotFound, 40420, err)
	case errors.Is(err, execute.ErrVersionConflict), errors.Is(err, execute.ErrInvalidTransition):
		writeAPIError(c, consts.StatusConflict, 40920, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50021, fmt.Errorf("execute Task failed: %s", strings.TrimSpace(err.Error())))
	}
}

func GetTaskRun(service execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, err := strconv.ParseUint(c.Param("run_id"), 10, 64)
		if err != nil || id == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40025, fmt.Errorf("invalid run_id"))
			return
		}
		result, err := service.GetRun(ctx, id)
		if err != nil {
			switch {
			case errors.Is(err, execute.ErrRunNotFound):
				writeAPIError(c, consts.StatusNotFound, 40425, err)
			case errors.Is(err, execute.ErrInvalidInput):
				writeAPIError(c, consts.StatusBadRequest, 40025, err)
			default:
				writeAPIError(c, consts.StatusInternalServerError, 50025, err)
			}
			return
		}
		if c.Query("include_prompt") != "true" {
			result.Prompt = ""
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
