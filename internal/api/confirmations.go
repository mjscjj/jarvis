package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"jarvis/internal/decide"
	"jarvis/internal/extract"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type approveConfirmationRequest struct {
	ExpectedVersion *int32          `json:"expected_version"`
	Plan            json.RawMessage `json:"plan"`
}

type rejectConfirmationRequest struct {
	ExpectedVersion *int32 `json:"expected_version"`
	Reason          string `json:"reason"`
}

var confirmationStatuses = map[string]struct{}{
	"need_info": {}, "need_decision": {},
}

func ListConfirmations(reader extract.TodoReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		filter, err := todoListFilter(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40014, err)
			return
		}
		if len(filter.Statuses) == 0 {
			filter.Statuses = []string{"need_info", "need_decision"}
		}
		for _, status := range filter.Statuses {
			if _, ok := confirmationStatuses[status]; !ok {
				writeAPIError(c, consts.StatusBadRequest, 40014, fmt.Errorf("confirmation status must be need_info or need_decision"))
				return
			}
		}
		if err := extract.ValidateTodoFilter(filter); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40014, err)
			return
		}
		result, err := reader.ListTodos(ctx, filter)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50011, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetConfirmation(reader decide.ConfirmationDetailReader) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		todoID, err := confirmationTodoID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40010, err)
			return
		}
		result, err := reader.GetConfirmation(ctx, todoID)
		if err != nil {
			writeConfirmationError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func ApproveConfirmation(service decide.ConfirmationService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		todoID, err := confirmationTodoID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40010, err)
			return
		}
		var request approveConfirmationRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40011, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40011, fmt.Errorf("expected_version is required"))
			return
		}
		result, err := service.Approve(ctx, decide.ApproveInput{
			TodoID: todoID, ExpectedVersion: *request.ExpectedVersion, Plan: request.Plan, Channel: "backend",
		})
		if err != nil {
			writeConfirmationError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func RejectConfirmation(service decide.ConfirmationService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		todoID, err := confirmationTodoID(c)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40010, err)
			return
		}
		var request rejectConfirmationRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40012, err)
			return
		}
		if request.ExpectedVersion == nil {
			writeAPIError(c, consts.StatusBadRequest, 40012, fmt.Errorf("expected_version is required"))
			return
		}
		result, err := service.Reject(ctx, decide.RejectInput{
			TodoID: todoID, ExpectedVersion: *request.ExpectedVersion, Reason: request.Reason, Channel: "backend",
		})
		if err != nil {
			writeConfirmationError(c, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func confirmationTodoID(c *app.RequestContext) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("todo_id"), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("todo_id must be a positive integer")
	}
	return id, nil
}

func decodeStrictJSON(body []byte, target any) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing request body: %w", err)
	}
	return nil
}

func writeConfirmationError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, decide.ErrInvalidInput):
		writeAPIError(c, consts.StatusBadRequest, 40013, err)
	case errors.Is(err, decide.ErrTodoNotFound):
		writeAPIError(c, consts.StatusNotFound, 40410, err)
	case errors.Is(err, decide.ErrVersionConflict),
		errors.Is(err, decide.ErrInvalidTransition),
		errors.Is(err, decide.ErrTaskExists):
		writeAPIError(c, consts.StatusConflict, 40910, err)
	default:
		writeAPIError(c, consts.StatusInternalServerError, 50010, fmt.Errorf("confirmation failed: %s", strings.TrimSpace(err.Error())))
	}
}
