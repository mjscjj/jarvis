package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/taskcreate"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type createTaskRequest struct {
	Title         string          `json:"title"`
	ActionType    string          `json:"action_type"`
	Target        string          `json:"target"`
	Background    json.RawMessage `json:"background"`
	Plan          json.RawMessage `json:"plan"`
	ExecutionMode string          `json:"execution_mode"`
	ProjectID     *uint64         `json:"project_id"`
}

func CreateTask(submitter *taskcreate.Submitter) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request createTaskRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40029, err)
			return
		}
		mode := strings.TrimSpace(request.ExecutionMode)
		if mode == "" {
			mode = taskcreate.ExecutionModeDirect
		}
		task, err := submitter.Submit(ctx, taskcreate.Input{
			Title: request.Title, ActionType: request.ActionType, Target: request.Target,
			Background: request.Background, Plan: request.Plan, ConfirmedBy: "user",
			ProjectID: request.ProjectID, SourceType: taskcreate.SourceManual,
			ExecutionMode: mode, ActorType: "user",
			EventDetail: map[string]any{"channel": "backend"},
		})
		if err != nil {
			if errors.Is(err, taskcreate.ErrInvalidInput) {
				writeAPIError(c, consts.StatusBadRequest, 40029, err)
			} else if errors.Is(err, taskcreate.ErrExists) {
				writeAPIError(c, consts.StatusConflict, 40929, err)
			} else {
				writeAPIError(c, consts.StatusInternalServerError, 50029, fmt.Errorf("create Task: %w", err))
			}
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": map[string]any{
			"id": task.ID, "todo_id": task.TodoID, "title": task.Title,
			"action_type": task.ActionType, "target": task.Target, "status": task.Status,
			"source_type": task.SourceType, "source_id": task.SourceID,
			"occurrence_key": task.OccurrenceKey, "execution_mode": task.ExecutionMode,
			"version": task.Version,
		}})
	}
}
