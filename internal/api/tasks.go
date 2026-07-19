package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"jarvis/internal/execute"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
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
		result, err := service.Finish(ctx, execute.FinishInput{
			TaskID: taskID, ExpectedVersion: *request.ExpectedVersion,
			Status: strings.TrimSpace(request.Status), Result: request.Result,
		})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// ExecuteTask triggers agent-driven execution of a confirmed Task. The manual
// click is treated as approval for external-side-effect actions.
func ExecuteTask(executor *execute.AgentExecutor) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40023, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		result, err := executor.Execute(ctx, execute.ExecuteInput{TaskID: taskID, ApproveExternal: true})
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

// RerunTask re-executes a finished (done/failed) Task. The manual click counts
// as approval for external-side-effect actions.
func RerunTask(executor *execute.AgentExecutor) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		taskID, err := strconv.ParseUint(c.Param("task_id"), 10, 64)
		if err != nil || taskID == 0 {
			writeAPIError(c, consts.StatusBadRequest, 40023, fmt.Errorf("task_id must be a positive integer"))
			return
		}
		result, err := executor.Rerun(ctx, taskID)
		if err != nil {
			writeExecutionError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func writeExecutionError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, execute.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40022, err)
	case errors.Is(err, execute.ErrTaskNotFound):
		writeAPIError(c, consts.StatusNotFound, 40420, err)
	case errors.Is(err, execute.ErrVersionConflict), errors.Is(err, execute.ErrInvalidTransition):
		writeAPIError(c, consts.StatusConflict, 40920, err)
	case errors.Is(err, execute.ErrExternalNeedsApproval):
		writeAPIError(c, consts.StatusConflict, 40923, err)
	case errors.Is(err, execute.ErrUnknownActionType):
		writeAPIError(c, consts.StatusBadRequest, 40024, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50021, fmt.Errorf("execute Task failed: %s", strings.TrimSpace(err.Error())))
	}
}
