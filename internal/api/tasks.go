package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/execute"

	"code.byted.org/middleware/hertz/pkg/app"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
)

type finishTaskRequest struct {
	ExpectedVersion *int32          `json:"expected_version"`
	Status          string          `json:"status"`
	Result          json.RawMessage `json:"result"`
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
		result, err := service.ListTasks(ctx, execute.TaskFilter{Statuses: statuses, Page: page, PageSize: pageSize})
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

// ListTaskRuns returns a Task's execution audit history (ExecutionRun list),
// newest first, powering the task detail drawer's execution timeline.
func ListTaskRuns(service execute.TaskService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40025, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		result, err := service.ListRuns(ctx, taskID)
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

// ExecuteTask triggers agent-driven execution of a confirmed Task. Local actions
// run to completion; external-side-effect actions run the propose stage — the
// agent judges risk and either finishes low-risk work or produces a proposal and
// parks the Task at awaiting_approval for a human to approve (ApproveTask) or
// reject (RejectTask). The click no longer directly lands external writes.
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

type approveTaskRequest struct {
	ExpectedVersion *int32 `json:"expected_version"`
}

// ApproveTask lands a proposal a human accepted: the awaiting_approval Task is
// claimed synchronously (-> executing) and the apply stage (a fresh codex
// invocation carrying the approved proposal) runs in the background. The handler
// returns as soon as the claim succeeds; poll Task status for the final verdict.
func ApproveTask(executor *execute.AgentExecutor) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40027, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		var request approveTaskRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40027, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40027, fmt.Errorf("expected_version is required"))
			return
		}
		result, err := executor.KickApprove(ctx, taskID, *request.ExpectedVersion)
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

type rejectTaskRequest struct {
	ExpectedVersion *int32 `json:"expected_version"`
	Reason          string `json:"reason"`
}

// RejectTask declines a proposed external write: the awaiting_approval Task moves
// to failed with the rejection reason recorded; it can later be rerun.
func RejectTask(executor *execute.AgentExecutor) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40028, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		var request rejectTaskRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40028, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40028, fmt.Errorf("expected_version is required"))
			return
		}
		result, err := executor.Reject(ctx, taskID, *request.ExpectedVersion, request.Reason)
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// RerunTask re-executes a finished (done/failed) Task. The manual click counts
// as approval for external-side-effect actions. Persisted execution_supplements
// are included automatically on every run.
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

// ReapplyTask re-lands the same human-approved proposal for a Task whose apply
// stage previously failed, WITHOUT going through propose/approval again. It is
// the "用同一已批准方案重试落地" shortcut, distinct from RerunTask (which restarts
// from propose and re-requests approval).
func ReapplyTask(executor *execute.AgentExecutor) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40029, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		result, err := executor.KickReapply(ctx, taskID)
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

// ResumeTaskAfterHuman continues the exact Codex session that asked for human
// input. It is deliberately distinct from rerun/reapply: no Task plan or
// approved artifact is regenerated.
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
		result, err := executor.KickResumeAfterHuman(ctx, taskID, *request.ExpectedVersion, request.Response)
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
// execution_supplements. It does not trigger execution or M4 re-evaluation.
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
func RecallEffectMessage(recaller *execute.MessageRecaller) app.HandlerFunc {
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
		result, err := recaller.Recall(ctx, taskID, request.MessageID)
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// manualStage maps a manual finish status to the execution_result stage tag so
// the UI distinguishes a human-marked failure from a codex execution failure.
func manualStage(status string) string {
	if status == "failed" {
		return "manual_failed"
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
	case errors.Is(err, execute.ErrRecallTargetNotFound):
		writeAPIError(c, consts.StatusNotFound, 40421, err)
	case errors.Is(err, execute.ErrMessageAlreadyRecalled):
		writeAPIError(c, consts.StatusConflict, 40921, err)
	case errors.Is(err, execute.ErrVersionConflict), errors.Is(err, execute.ErrInvalidTransition):
		writeAPIError(c, consts.StatusConflict, 40920, err)
	case errors.Is(err, execute.ErrUnknownActionType):
		writeAPIError(c, consts.StatusBadRequest, 40024, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50021, fmt.Errorf("execute Task failed: %s", strings.TrimSpace(err.Error())))
	}
}
